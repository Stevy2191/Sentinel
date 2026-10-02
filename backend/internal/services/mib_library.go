package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/mib"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

var (
	ErrMIBNotFound = errors.New("MIB module not found")
	ErrMIBBuiltin  = errors.New("built-in MIB modules cannot be deleted")
)

// MIBUploadError names the uploaded file that failed.
type MIBUploadError struct {
	File string
	Err  error
}

func (e *MIBUploadError) Error() string { return e.File + ": " + e.Err.Error() }
func (e *MIBUploadError) Unwrap() error { return e.Err }

// MIBInUseError lists what still depends on a module.
type MIBInUseError struct {
	Modules []string
	Metrics []string
}

func (e *MIBInUseError) Error() string {
	return fmt.Sprintf("in use by modules %v and metrics %v", e.Modules, e.Metrics)
}

type MIBUploadResult struct {
	Saved   []string            `json:"saved"`
	Skipped []string            `json:"skipped"`
	Waiting map[string][]string `json:"waiting"`
}

type MIBModuleView struct {
	models.MIBModule
	Objects int `json:"objects" gorm:"column:objects"`
}

// MIBLibrary stores MIB modules and their objects. Every change rebuilds the
// whole object tree (modules number in the tens), so a module waiting for an
// import becomes ready the moment the import arrives.
type MIBLibrary struct {
	db *gorm.DB
	mu sync.Mutex // one rebuild at a time
}

func NewMIBLibrary(db *gorm.DB) *MIBLibrary { return &MIBLibrary{db: db} }

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// SyncBuiltins inserts or refreshes the embedded modules — unless an upload
// of the same name replaced one — and rebuilds when anything changed.
func (l *MIBLibrary) SyncBuiltins(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	changed := false
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, f := range mib.Builtins() {
			var existing models.MIBModule
			err := tx.Where("name = ?", f.Name).Limit(1).Find(&existing).Error
			if err != nil {
				return err
			}
			h := hashOf(f.Content)
			if existing.ID != uuid.Nil && (existing.Source == models.MIBSourceUpload || existing.SHA256 == h) {
				continue
			}
			if err := upsertModule(tx, f, models.MIBSourceBuiltin, f.Name+".txt", nil); err != nil {
				return err
			}
			changed = true
		}
		var n int64
		if err := tx.Model(&models.MIBObject{}).Count(&n).Error; err != nil {
			return err
		}
		if changed || n == 0 {
			return rebuild(tx)
		}
		return nil
	})
	return err
}

func upsertModule(tx *gorm.DB, f mib.File, source, fileName string, by *uuid.UUID) error {
	h, _ := mib.Inspect([]byte(f.Content))
	now := time.Now().UTC()
	return tx.Exec(`INSERT INTO mib_modules (name, source, file_name, size_bytes, sha256, content, imports, status, uploaded_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'waiting', ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET source = EXCLUDED.source, file_name = EXCLUDED.file_name,
			size_bytes = EXCLUDED.size_bytes, sha256 = EXCLUDED.sha256, content = EXCLUDED.content,
			imports = EXCLUDED.imports, uploaded_by = EXCLUDED.uploaded_by, updated_at = EXCLUDED.updated_at`,
		f.Name, source, fileName, len(f.Content), hashOf(f.Content), f.Content, models.StringArray(h.Imports), by, now, now).Error
}

// rebuild recomputes every module's status and replaces all objects.
func rebuild(tx *gorm.DB) error {
	var mods []models.MIBModule
	if err := tx.Find(&mods).Error; err != nil {
		return err
	}
	files := make([]mib.File, len(mods))
	ids := map[string]uuid.UUID{}
	for i, m := range mods {
		files[i] = mib.File{Name: m.Name, Content: m.Content}
		ids[m.Name] = m.ID
	}
	res := mib.Build(files)
	for _, m := range mods {
		status := models.MIBStatusReady
		if len(res.Missing[m.Name]) > 0 {
			status = models.MIBStatusWaiting
		}
		if err := tx.Model(&models.MIBModule{}).Where("id = ?", m.ID).
			Updates(map[string]any{"status": status, "missing": models.StringArray(res.Missing[m.Name])}).Error; err != nil {
			return err
		}
	}
	if err := tx.Exec(`DELETE FROM mib_objects`).Error; err != nil {
		return err
	}
	var rows []models.MIBObject
	for name, objs := range res.Objects {
		for _, o := range objs {
			rows = append(rows, models.MIBObject{ModuleID: ids[name], Name: o.Name, OID: o.OID, ParentOID: o.ParentOID,
				Kind: o.Kind, BaseType: o.BaseType, TypeName: o.TypeName, Units: o.Units, Access: o.Access,
				Description: o.Description, Enum: models.EnumMap(o.Enum), IndexColumns: models.StringArray(o.IndexColumns)})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.CreateInBatches(rows, 500).Error
}

// Upload saves every MIB in files, or none: a file that fails to parse
// rejects the whole upload. Files that are not MIBs at all are skipped.
func (l *MIBLibrary) Upload(ctx context.Context, files []UploadFile, by uuid.UUID) (*MIBUploadResult, error) {
	expanded, err := ExpandUpload(files)
	if err != nil {
		return nil, err
	}
	res := &MIBUploadResult{Waiting: map[string][]string{}}
	var mods []mib.File
	fileNames := map[string]string{}
	// paths is where each module came from (its path inside a zip), for
	// messages: two vendor folders often hold the same base name.
	paths := map[string]string{}
	for _, f := range expanded {
		where := f.Path
		if where == "" {
			where = f.FileName
		}
		h, err := mib.Inspect(f.Content)
		if errors.Is(err, mib.ErrNotAMIB) {
			res.Skipped = append(res.Skipped, f.FileName)
			continue
		}
		if err != nil {
			return nil, &MIBUploadError{File: where, Err: err}
		}
		if prev, dup := paths[h.Name]; dup {
			return nil, &MIBUploadError{File: where, Err: fmt.Errorf("module %s is also in %s", h.Name, prev)}
		}
		fileNames[h.Name] = f.FileName
		paths[h.Name] = where
		mods = append(mods, mib.File{Name: h.Name, Content: string(f.Content)})
	}
	var byPtr *uuid.UUID
	if by != uuid.Nil {
		byPtr = &by
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err = l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, m := range mods {
			if err := upsertModule(tx, m, models.MIBSourceUpload, fileNames[m.Name], byPtr); err != nil {
				return err
			}
		}
		if err := rebuild(tx); err != nil {
			return err
		}
		var waiting []models.MIBModule
		if err := tx.Where("name IN ? AND status = ?", keysOf(fileNames), models.MIBStatusWaiting).Find(&waiting).Error; err != nil {
			return err
		}
		for _, w := range waiting {
			res.Waiting[w.Name] = w.Missing
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, m := range mods {
		res.Saved = append(res.Saved, m.Name)
	}
	sort.Strings(res.Saved)
	return res, nil
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

// Delete removes an uploaded module unless something depends on it. An
// upload that had replaced a built-in brings the built-in back.
func (l *MIBLibrary) Delete(ctx context.Context, id uuid.UUID) (*models.MIBModule, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var m models.MIBModule
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", id).Limit(1).Find(&m).Error; err != nil {
			return err
		}
		if m.ID == uuid.Nil {
			return ErrMIBNotFound
		}
		if m.Source == models.MIBSourceBuiltin {
			return ErrMIBBuiltin
		}
		inUse := &MIBInUseError{}
		if err := tx.Raw(`SELECT name FROM mib_modules WHERE ? = ANY(imports) ORDER BY name`, m.Name).Scan(&inUse.Modules).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT DISTINCT pm.key FROM profile_metrics pm JOIN mib_objects o ON o.module_id = ?
			AND o.oid IN (pm.oid, pm.oid2, pm.precision_oid, pm.filter_oid, pm.label_oid, pm.label_pointer_oid, pm.label_target_oid)
			ORDER BY pm.key`, m.ID).Scan(&inUse.Metrics).Error; err != nil {
			return err
		}
		if len(inUse.Modules) > 0 || len(inUse.Metrics) > 0 {
			return inUse
		}
		if err := tx.Delete(&models.MIBModule{}, "id = ?", m.ID).Error; err != nil {
			return err
		}
		for _, f := range mib.Builtins() {
			if f.Name == m.Name {
				if err := upsertModule(tx, f, models.MIBSourceBuiltin, f.Name+".txt", nil); err != nil {
					return err
				}
			}
		}
		return rebuild(tx)
	})
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// List returns every module with its object count, by name.
func (l *MIBLibrary) List(ctx context.Context) ([]MIBModuleView, error) {
	var out []MIBModuleView
	err := l.db.WithContext(ctx).Raw(`SELECT m.*, (SELECT count(*) FROM mib_objects o WHERE o.module_id = m.id) AS objects
		FROM mib_modules m ORDER BY m.name`).Scan(&out).Error
	return out, err
}
