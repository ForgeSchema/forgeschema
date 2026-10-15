// Package postgres inspects PostgreSQL schemas and applies supported reviewed migration plans.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ForgeSchema/forgeschema/schema"
	_ "github.com/lib/pq"
)

// UnsupportedError identifies an object the schema contract cannot represent.
// Callers must not use a partial document after any inspection error.
type UnsupportedError struct{ Object, Feature string }

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("unsupported %s: %s", e.Object, e.Feature)
}
func unsupported(object, feature string) error { return &UnsupportedError{object, feature} }

// Inspect reads one existing schema in a single READ ONLY, REPEATABLE READ
// transaction. PostgreSQL 16 and 17 are supported; other major versions fail.
// The caller owns db and should supply a context deadline. No rows of user data
// are read. This is a metadata snapshot, not a lock against future schema drift.
func Inspect(ctx context.Context, db *sql.DB, name string) (schema.Document, error) {
	empty := schema.Document{}
	if _, err := schema.Normalize(schema.Document{Version: 1, Schema: name, Tables: []schema.Table{}}); err != nil {
		return empty, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	doc, err := inspectTx(ctx, tx, name)
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return doc, nil
}

// inspectTx uses the caller's transaction snapshot. Apply uses READ COMMITTED
// and calls this only after acquiring relation locks, then again after DDL.
func inspectTx(ctx context.Context, tx *sql.Tx, name string) (schema.Document, error) {
	empty := schema.Document{}
	// Qualify every object and prevent user objects shadowing catalog functions.
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path = pg_catalog"); err != nil {
		return empty, err
	}
	var err error
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT current_setting('server_version_num')::integer").Scan(&version); err != nil {
		return empty, err
	}
	if version < 160000 || version >= 180000 {
		return empty, unsupported("server", "only PostgreSQL 16 and 17 are supported")
	}
	var oid int64
	if err = tx.QueryRowContext(ctx, "SELECT oid FROM pg_catalog.pg_namespace WHERE nspname=$1", name).Scan(&oid); err == sql.ErrNoRows {
		return empty, unsupported("schema "+name, "schema does not exist")
	}
	if err != nil {
		return empty, err
	}
	if err = checkScope(ctx, tx, oid, name); err != nil {
		return empty, err
	}
	doc, err := readTables(ctx, tx, oid, name)
	if err != nil {
		return empty, err
	}
	normalized, err := schema.Normalize(doc)
	if err != nil {
		return empty, unsupported("schema "+name, err.Error())
	}
	return normalized, nil
}

func checkScope(ctx context.Context, tx *sql.Tx, oid int64, name string) error {
	// These objects would otherwise be invisible in a table-only snapshot.
	checks := []struct{ query, feature string }{
		{`SELECT relname FROM pg_catalog.pg_class WHERE relnamespace=$1 AND relkind NOT IN ('r','i') ORDER BY relname LIMIT 1`, "views, sequences, foreign and partitioned relations are not represented"},
		{`SELECT typname FROM pg_catalog.pg_type WHERE typnamespace=$1 AND typtype<>'c' AND typelem=0 ORDER BY typname LIMIT 1`, "custom types and domains are not represented"},
		{`SELECT p.proname FROM pg_catalog.pg_proc p WHERE p.pronamespace=$1 ORDER BY p.proname LIMIT 1`, "functions and procedures are not represented"},
		{`SELECT collname FROM pg_catalog.pg_collation WHERE collnamespace=$1 ORDER BY collname LIMIT 1`, "custom collations are not represented"},
		{`SELECT conname FROM pg_catalog.pg_conversion WHERE connamespace=$1 ORDER BY conname LIMIT 1`, "custom conversions are not represented"},
		{`SELECT s.stxname FROM pg_catalog.pg_statistic_ext s WHERE s.stxnamespace=$1 ORDER BY s.stxname LIMIT 1`, "extended statistics are not represented"},
		{`SELECT c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_depend d ON d.classid='pg_catalog.pg_class'::regclass AND d.objid=c.oid AND d.deptype='e' WHERE c.relnamespace=$1 ORDER BY c.relname LIMIT 1`, "extension-owned relations are not represented"},
	}
	for _, c := range checks {
		var object string
		err := tx.QueryRowContext(ctx, c.query, oid).Scan(&object)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		return unsupported(name+"."+object, c.feature)
	}
	return nil
}

func readTables(ctx context.Context, tx *sql.Tx, oid int64, name string) (schema.Document, error) {
	doc := schema.Document{Version: 1, Schema: name, Tables: []schema.Table{}}
	rows, err := tx.QueryContext(ctx, `SELECT c.oid,c.relname,
 c.relpersistence<>'p' OR c.relispartition OR c.relrowsecurity OR c.relforcerowsecurity
 OR c.relreplident<>'d' OR c.reltablespace<>0 OR c.reloptions IS NOT NULL OR am.amname<>'heap'
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_inherits h WHERE h.inhrelid=c.oid OR h.inhparent=c.oid)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_trigger t WHERE t.tgrelid=c.oid AND NOT t.tgisinternal)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_rewrite r WHERE r.ev_class=c.oid)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_policy p WHERE p.polrelid=c.oid)
 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_am am ON am.oid=c.relam
 WHERE c.relnamespace=$1 AND c.relkind='r' ORDER BY c.relname`, oid)
	if err != nil {
		return doc, err
	}
	type tableInfo struct {
		oid     int64
		name    string
		special bool
	}
	var tables []tableInfo
	for rows.Next() {
		var t tableInfo
		if err = rows.Scan(&t.oid, &t.name, &t.special); err != nil {
			rows.Close()
			return doc, err
		}
		tables = append(tables, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return doc, err
	}
	for _, info := range tables {
		object := name + "." + info.name
		if info.special {
			return doc, unsupported(object, "nonstandard table storage, inheritance, partitioning, policies, rules or triggers")
		}
		table := schema.Table{Name: info.name}
		if table.Columns, err = readColumns(ctx, tx, info.oid, object); err != nil {
			return doc, err
		}
		if table.PrimaryKey, err = readConstraints(ctx, tx, info.oid, object, info.name); err != nil {
			return doc, err
		}
		if table.Indexes, err = readIndexes(ctx, tx, info.oid, object); err != nil {
			return doc, err
		}
		doc.Tables = append(doc.Tables, table)
	}
	return doc, nil
}
func readColumns(ctx context.Context, tx *sql.Tx, oid int64, object string) ([]schema.Column, error) {
	rows, err := tx.QueryContext(ctx, `SELECT a.attname,pg_catalog.format_type(a.atttypid,a.atttypmod),NOT a.attnotnull,
 a.atthasdef OR a.attidentity<>'' OR a.attgenerated<>'' OR a.attcollation<>t.typcollation
 OR a.attstorage<>t.typstorage OR a.attcompression<>'' OR a.attoptions IS NOT NULL
 OR a.attfdwoptions IS NOT NULL OR tn.nspname<>'pg_catalog' OR t.typtype<>'b' OR t.typelem<>0
 FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_type t ON t.oid=a.atttypid
 JOIN pg_catalog.pg_namespace tn ON tn.oid=t.typnamespace
 WHERE a.attrelid=$1 AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`, oid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []schema.Column
	for rows.Next() {
		var c schema.Column
		var special bool
		if err = rows.Scan(&c.Name, &c.Type, &c.Nullable, &special); err != nil {
			return nil, err
		}
		if special {
			return nil, unsupported(object+"."+c.Name, "defaults, identity/generated columns, custom types, collations or storage options")
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}
func readConstraints(ctx context.Context, tx *sql.Tx, oid int64, object, table string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT c.conname,c.contype::text,c.condeferrable OR c.condeferred OR NOT c.convalidated OR NOT c.conislocal OR c.coninhcount<>0,
 COALESCE((SELECT json_agg(a.attname ORDER BY k.n)::text FROM unnest(c.conkey) WITH ORDINALITY k(attnum,n)
 JOIN pg_catalog.pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum),'[]')
 FROM pg_catalog.pg_constraint c WHERE c.conrelid=$1 ORDER BY c.conname`, oid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pk []string
	for rows.Next() {
		var cname, kind, columns string
		var special bool
		if err = rows.Scan(&cname, &kind, &special, &columns); err != nil {
			return nil, err
		}
		if kind != "p" || special || cname != table+"_pkey" {
			return nil, unsupported(object+" constraint "+cname, "only a nondeferrable TABLE_pkey primary key is represented")
		}
		if err = json.Unmarshal([]byte(columns), &pk); err != nil {
			return nil, err
		}
	}
	return pk, rows.Err()
}
func readIndexes(ctx context.Context, tx *sql.Tx, oid int64, object string) ([]schema.Index, error) {
	rows, err := tx.QueryContext(ctx, `SELECT ic.relname,i.indisprimary,
 am.amname<>'btree' OR (i.indisunique AND NOT i.indisprimary) OR i.indisexclusion
 OR NOT i.indisvalid OR NOT i.indisready OR NOT i.indislive OR NOT i.indimmediate
 OR i.indnullsnotdistinct OR i.indisclustered OR i.indisreplident
 OR i.indexprs IS NOT NULL OR i.indpred IS NOT NULL OR i.indnatts<>i.indnkeyatts
 OR ic.reloptions IS NOT NULL OR ic.reltablespace<>0
 OR EXISTS(SELECT 1 FROM unnest(i.indoption) opt WHERE opt<>0)
 OR EXISTS(SELECT 1 FROM unnest(i.indclass) opc JOIN pg_catalog.pg_opclass o ON o.oid=opc
 JOIN pg_catalog.pg_namespace n ON n.oid=o.opcnamespace WHERE NOT o.opcdefault OR n.nspname<>'pg_catalog')
 OR EXISTS(SELECT 1 FROM unnest(i.indkey) WITH ORDINALITY k(attnum,n)
 JOIN pg_catalog.pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum
 WHERE i.indcollation[(k.n-1)::integer]<>a.attcollation),
 COALESCE((SELECT json_agg(a.attname ORDER BY k.n)::text FROM unnest(i.indkey) WITH ORDINALITY k(attnum,n)
 JOIN pg_catalog.pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum),'[]')
 FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class ic ON ic.oid=i.indexrelid
 JOIN pg_catalog.pg_am am ON am.oid=ic.relam WHERE i.indrelid=$1 ORDER BY ic.relname`, oid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var indexes []schema.Index
	for rows.Next() {
		var idx schema.Index
		var primary, special bool
		var columns string
		if err = rows.Scan(&idx.Name, &primary, &special, &columns); err != nil {
			return nil, err
		}
		if special {
			return nil, unsupported(object+" index "+idx.Name, "only valid ordinary default-order btree indexes without options are represented")
		}
		if primary {
			continue
		}
		if err = json.Unmarshal([]byte(columns), &idx.Columns); err != nil {
			return nil, err
		}
		indexes = append(indexes, idx)
	}
	return indexes, rows.Err()
}
