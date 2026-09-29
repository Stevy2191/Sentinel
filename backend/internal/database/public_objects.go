package database

import (
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"
)

// nonTableObjectsQuery lists what a backup would silently miss: anything in
// public that is not a table or sequence and not owned by an extension
// (pgcrypto and timescaledb put their own functions there, and those are
// recreated by migrations, not restored from a backup).
const nonTableObjectsQuery = `
SELECT 'function public.' || p.proname
  FROM pg_proc p
 WHERE p.pronamespace = 'public'::regnamespace
   AND NOT EXISTS (SELECT 1 FROM pg_depend d
                    WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e')
UNION ALL
SELECT CASE c.relkind WHEN 'v' THEN 'view' WHEN 'm' THEN 'materialized view' ELSE 'type' END
       || ' public.' || c.relname
  FROM pg_class c
 WHERE c.relnamespace = 'public'::regnamespace
   AND c.relkind IN ('v', 'm', 'c')
   AND NOT EXISTS (SELECT 1 FROM pg_depend d
                    WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e')
UNION ALL
SELECT 'type public.' || t.typname
  FROM pg_type t
 WHERE t.typnamespace = 'public'::regnamespace
   AND t.typtype IN ('e', 'd')
   AND NOT EXISTS (SELECT 1 FROM pg_depend d
                    WHERE d.classid = 'pg_type'::regclass AND d.objid = t.oid AND d.deptype = 'e')
ORDER BY 1`

// NonTableObjectsWarning explains why the named objects are a problem, or
// returns "" when there are none.
func NonTableObjectsWarning(objects []string) string {
	if len(objects) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"WARNING: public contains objects that Settings -> Backups does not back up: %s. "+
			"Backups dump tables only (see BackupService.dumpArgs), so these would be missing "+
			"after a restore. Recreate them from a migration, or change the backup to cover them.",
		strings.Join(objects, ", "))
}

// WarnNonTableObjects logs NonTableObjectsWarning for the live database. It
// only warns: a missing function is a backup gap, not a reason to refuse to
// start. A failed check is logged and ignored for the same reason.
func WarnNonTableObjects(db *gorm.DB) {
	var objects []string
	if err := db.Raw(nonTableObjectsQuery).Scan(&objects).Error; err != nil {
		log.Printf("[backup] could not check public for non-table objects: %v", err)
		return
	}
	if msg := NonTableObjectsWarning(objects); msg != "" {
		log.Print(msg)
	}
}
