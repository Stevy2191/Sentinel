package api

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// fakeMIBs is an in-memory mibStore. uploadCalls counts every call to
// Upload, so a test can prove a refused request never reached the library.
type fakeMIBs struct {
	uploadCalls int
	uploadErr   error
	deleteErr   error
}

func (f *fakeMIBs) List(context.Context) ([]services.MIBModuleView, error) {
	return []services.MIBModuleView{}, nil
}
func (f *fakeMIBs) Upload(context.Context, []services.UploadFile, uuid.UUID) (*services.MIBUploadResult, error) {
	f.uploadCalls++
	if f.uploadErr != nil {
		return nil, f.uploadErr
	}
	return &services.MIBUploadResult{Saved: []string{"ACME-MIB"}}, nil
}
func (f *fakeMIBs) Delete(context.Context, uuid.UUID) (*models.MIBModule, error) {
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &models.MIBModule{Name: "ACME-MIB"}, nil
}
func (f *fakeMIBs) Children(context.Context, string) ([]services.MIBObjectView, error) {
	return []services.MIBObjectView{}, nil
}
func (f *fakeMIBs) Search(context.Context, string, int) ([]services.MIBObjectView, error) {
	return []services.MIBObjectView{}, nil
}
func (f *fakeMIBs) Object(context.Context, string) (*services.MIBObjectDetail, error) {
	return &services.MIBObjectDetail{}, nil
}

// mibRouter mounts the MIB routes as a signed-in user with the given admin
// claim. stubUsers (auth_middleware_test.go) confirms the claim.
func mibRouter(store *fakeMIBs, audit *fakeAudit, isAdmin bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	RegisterMIBRoutes(r.Group("/api/v1"), store, audit, stubUsers{user: &models.User{IsAdmin: isAdmin}})
	return r
}

func multipartUpload(t *testing.T, fileName, content string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("files", fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func TestMIBRoutesAccess(t *testing.T) {
	store := &fakeMIBs{}
	r := mibRouter(store, &fakeAudit{}, false)

	if w := do(r, http.MethodGet, "/api/v1/network/mibs", nil); w.Code != http.StatusForbidden {
		t.Errorf("non-admin list: %d, want 403", w.Code)
	}
	if w := do(r, http.MethodGet, "/api/v1/network/mibs/objects/search?q=ac", nil); w.Code != http.StatusOK {
		t.Errorf("non-admin search: %d, want 200", w.Code)
	}
	if w := do(r, http.MethodGet, "/api/v1/network/mibs/objects/children?oid=1.3.6.1.2.1", nil); w.Code != http.StatusOK {
		t.Errorf("non-admin children: %d, want 200", w.Code)
	}
	if w := do(r, http.MethodDelete, "/api/v1/network/mibs/"+uuid.New().String(), nil); w.Code != http.StatusForbidden {
		t.Errorf("non-admin delete: %d, want 403", w.Code)
	}
}

func TestMIBSearchRequiresTwoCharacters(t *testing.T) {
	store := &fakeMIBs{}
	r := mibRouter(store, &fakeAudit{}, false)
	if w := do(r, http.MethodGet, "/api/v1/network/mibs/objects/search?q=a", nil); w.Code != http.StatusBadRequest {
		t.Errorf("one-character search: %d, want 400", w.Code)
	}
}

func TestMIBUpload(t *testing.T) {
	store := &fakeMIBs{}
	audit := &fakeAudit{}
	r := mibRouter(store, audit, true)

	body, contentType := multipartUpload(t, "acme.my", "ACME-MIB DEFINITIONS ::= BEGIN END")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/mibs", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK || store.uploadCalls != 1 {
		t.Fatalf("upload: %d (want 200), calls %d (want 1), body %s", w.Code, store.uploadCalls, w.Body.String())
	}
	if len(audit.entries) != 1 || audit.entries[0] != models.ActionMIBUploaded {
		t.Errorf("audit entries %v", audit.entries)
	}
}

func TestMIBUploadNoFiles(t *testing.T) {
	store := &fakeMIBs{}
	r := mibRouter(store, &fakeAudit{}, true)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/mibs", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || store.uploadCalls != 0 {
		t.Errorf("empty upload: %d, calls %d", w.Code, store.uploadCalls)
	}
}

func TestMIBUploadError(t *testing.T) {
	store := &fakeMIBs{uploadErr: &services.MIBUploadError{File: "bad.my", Err: errors.New("line 3: unexpected")}}
	r := mibRouter(store, &fakeAudit{}, true)

	body, contentType := multipartUpload(t, "bad.my", "BAD-MIB DEFINITIONS ::= BEGIN")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/mibs", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "bad.my: line 3") {
		t.Errorf("upload error: %d, body %s", w.Code, w.Body.String())
	}
}

func TestMIBDeleteInUse(t *testing.T) {
	store := &fakeMIBs{deleteErr: &services.MIBInUseError{Modules: []string{"ACME-MIB"}}}
	r := mibRouter(store, &fakeAudit{}, true)

	w := do(r, http.MethodDelete, "/api/v1/network/mibs/"+uuid.New().String(), nil)
	if w.Code != http.StatusConflict {
		t.Errorf("delete in use: %d, want 409, body %s", w.Code, w.Body.String())
	}
}

func TestMIBDelete(t *testing.T) {
	store := &fakeMIBs{}
	audit := &fakeAudit{}
	r := mibRouter(store, audit, true)

	w := do(r, http.MethodDelete, "/api/v1/network/mibs/"+uuid.New().String(), nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ACME-MIB") {
		t.Errorf("delete: %d, body %s", w.Code, w.Body.String())
	}
	if len(audit.entries) != 1 || audit.entries[0] != models.ActionMIBDeleted {
		t.Errorf("audit entries %v", audit.entries)
	}
}
