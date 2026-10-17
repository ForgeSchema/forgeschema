// Package postgres inspects PostgreSQL schemas and applies supported reviewed migration plans.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

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
	// Standard strings keep backslashes literal in pg_get_expr output, whatever
	// the role, database or connection sets.
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path = pg_catalog; SET LOCAL standard_conforming_strings = on"); err != nil {
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
		{`SELECT c.relname FROM pg_catalog.pg_class c WHERE c.relnamespace=$1 AND c.relkind NOT IN ('r','i')
 AND NOT (c.relkind='S' AND EXISTS(SELECT 1 FROM pg_catalog.pg_depend d WHERE d.classid='pg_catalog.pg_class'::regclass
 AND d.objid=c.oid AND d.refclassid='pg_catalog.pg_class'::regclass AND d.deptype='i'))
 ORDER BY c.relname LIMIT 1`, "views, standalone or serial sequences, foreign and partitioned relations are not represented"},
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
		if table.Columns, err = readColumns(ctx, tx, oid, info.oid, object, info.name); err != nil {
			return doc, err
		}
		if table.PrimaryKey, table.Unique, table.ForeignKeys, err = readConstraints(ctx, tx, oid, info.oid, object, info.name); err != nil {
			return doc, err
		}
		if table.Indexes, err = readIndexes(ctx, tx, info.oid, object); err != nil {
			return doc, err
		}
		doc.Tables = append(doc.Tables, table)
	}
	return doc, nil
}
func readColumns(ctx context.Context, tx *sql.Tx, nsoid, oid int64, object, table string) ([]schema.Column, error) {
	// Defaults are rendered with relation 0: they cannot reference columns, and
	// passing the table would make pg_get_expr lock it, so inspection would
	// wait behind any session holding an exclusive lock on an unrelated table.
	// An identity column is accepted only with PostgreSQL's default sequence:
	// the deterministic TABLE_COLUMN_seq name in the same schema and default
	// start, increment, bounds, cache and cycle settings.
	rows, err := tx.QueryContext(ctx, `SELECT a.attname,pg_catalog.format_type(a.atttypid,a.atttypmod),NOT a.attnotnull,
 COALESCE(CASE WHEN a.attgenerated='' THEN pg_catalog.pg_get_expr(ad.adbin,0) END,''),a.attidentity::text,
 a.attidentity='' OR EXISTS(SELECT 1 FROM pg_catalog.pg_depend d JOIN pg_catalog.pg_class s ON s.oid=d.objid
  JOIN pg_catalog.pg_sequence q ON q.seqrelid=s.oid
  WHERE d.classid='pg_catalog.pg_class'::regclass AND d.refclassid='pg_catalog.pg_class'::regclass AND d.refobjid=a.attrelid
  AND d.refobjsubid=a.attnum AND d.deptype='i' AND s.relnamespace=$2 AND s.relname=$3||'_'||a.attname||'_seq'
  AND s.relpersistence='p' AND q.seqtypid=a.atttypid AND q.seqstart=1 AND q.seqincrement=1 AND q.seqmin=1 AND q.seqcache=1 AND NOT q.seqcycle
  AND q.seqmax=CASE a.atttypid WHEN 'pg_catalog.int2'::regtype THEN 32767 WHEN 'pg_catalog.int4'::regtype THEN 2147483647 ELSE 9223372036854775807 END),
 a.attgenerated<>'' OR a.attcollation<>t.typcollation
 OR a.attstorage<>t.typstorage OR a.attcompression<>'' OR a.attoptions IS NOT NULL
 OR a.attfdwoptions IS NOT NULL OR tn.nspname<>'pg_catalog' OR t.typtype<>'b' OR t.typelem<>0
 FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_type t ON t.oid=a.atttypid
 JOIN pg_catalog.pg_namespace tn ON tn.oid=t.typnamespace
 LEFT JOIN pg_catalog.pg_attrdef ad ON ad.adrelid=a.attrelid AND ad.adnum=a.attnum
 WHERE a.attrelid=$1 AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`, oid, nsoid, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []schema.Column
	for rows.Next() {
		var c schema.Column
		var expr, identity string
		var identityOK, special bool
		if err = rows.Scan(&c.Name, &c.Type, &c.Nullable, &expr, &identity, &identityOK, &special); err != nil {
			return nil, err
		}
		if special {
			return nil, unsupported(object+"."+c.Name, "generated columns, custom types, collations or storage options")
		}
		switch identity {
		case "a":
			c.Identity = "always"
		case "d":
			c.Identity = "by_default"
		}
		if !identityOK {
			return nil, unsupported(object+"."+c.Name, "identity columns need the default TABLE_COLUMN_seq sequence with default options")
		}
		if expr != "" {
			typ, ok := schema.NormalizeType(c.Type)
			d, supported := schema.DefaultFromCatalog(typ, expr)
			if !ok || !supported {
				feature := "default " + expr + " is outside the supported literal/function defaults"
				if strings.HasPrefix(expr, "nextval(") {
					feature += "; serial columns are unsupported, use identity"
				}
				return nil, unsupported(object+"."+c.Name, feature)
			}
			c.Default = d
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

var actionCodes = map[string]string{"a": "", "r": "restrict", "c": "cascade", "n": "set_null", "d": "set_default"}

func readConstraints(ctx context.Context, tx *sql.Tx, nsoid, oid int64, object, table string) ([]string, []schema.Unique, []schema.ForeignKey, error) {
	rows, err := tx.QueryContext(ctx, `SELECT c.conname,c.contype::text,
 c.condeferrable OR c.condeferred OR NOT c.convalidated OR NOT c.conislocal OR c.coninhcount<>0
 OR (c.contype='f' AND (c.confmatchtype<>'s' OR c.confdelsetcols IS NOT NULL OR r.relnamespace<>$2)),
 COALESCE((SELECT json_agg(a.attname ORDER BY k.n)::text FROM unnest(c.conkey) WITH ORDINALITY k(attnum,n)
 JOIN pg_catalog.pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=k.attnum),'[]'),
 COALESCE(r.relname::text,''),
 COALESCE((SELECT json_agg(a.attname ORDER BY k.n)::text FROM unnest(c.confkey) WITH ORDINALITY k(attnum,n)
 JOIN pg_catalog.pg_attribute a ON a.attrelid=c.confrelid AND a.attnum=k.attnum),'[]'),
 c.confdeltype::text,c.confupdtype::text
 FROM pg_catalog.pg_constraint c LEFT JOIN pg_catalog.pg_class r ON r.oid=c.confrelid
 WHERE c.conrelid=$1 ORDER BY c.conname`, oid, nsoid)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	var pk []string
	var uniques []schema.Unique
	var fks []schema.ForeignKey
	for rows.Next() {
		var cname, kind, cols, refTable, refCols, onDelete, onUpdate string
		var special bool
		if err = rows.Scan(&cname, &kind, &special, &cols, &refTable, &refCols, &onDelete, &onUpdate); err != nil {
			return nil, nil, nil, err
		}
		label := object + " constraint " + cname
		if special {
			return nil, nil, nil, unsupported(label, "deferrable, unvalidated, inherited, cross-schema, MATCH FULL or column-list SET NULL constraints are not represented")
		}
		var names []string
		if err = json.Unmarshal([]byte(cols), &names); err != nil {
			return nil, nil, nil, err
		}
		switch kind {
		case "p":
			if cname != table+"_pkey" {
				return nil, nil, nil, unsupported(label, "primary keys must use the TABLE_pkey name")
			}
			pk = names
		case "u":
			uniques = append(uniques, schema.Unique{Name: cname, Columns: names})
		case "f":
			fk := schema.ForeignKey{Name: cname, Columns: names, References: schema.Reference{Table: refTable}}
			if err = json.Unmarshal([]byte(refCols), &fk.References.Columns); err != nil {
				return nil, nil, nil, err
			}
			var ok1, ok2 bool
			fk.OnDelete, ok1 = actionCodes[onDelete]
			fk.OnUpdate, ok2 = actionCodes[onUpdate]
			if !ok1 || !ok2 {
				return nil, nil, nil, unsupported(label, "unknown referential action")
			}
			fks = append(fks, fk)
		default:
			return nil, nil, nil, unsupported(label, "check, exclusion and trigger constraints are not represented")
		}
	}
	return pk, uniques, fks, rows.Err()
}
func readIndexes(ctx context.Context, tx *sql.Tx, oid int64, object string) ([]schema.Index, error) {
	rows, err := tx.QueryContext(ctx, `SELECT ic.relname,
 i.indisprimary OR EXISTS(SELECT 1 FROM pg_catalog.pg_constraint c WHERE c.conindid=i.indexrelid AND c.conrelid=i.indrelid AND c.contype IN ('p','u')),
 i.indisunique,
 am.amname<>'btree' OR i.indisexclusion
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
		var constraint, special bool
		var columns string
		if err = rows.Scan(&idx.Name, &constraint, &idx.Unique, &special, &columns); err != nil {
			return nil, err
		}
		if special {
			return nil, unsupported(object+" index "+idx.Name, "only valid ordinary default-order btree indexes without options are represented")
		}
		// Primary key and unique constraint indexes are described by their constraints.
		if constraint {
			continue
		}
		if err = json.Unmarshal([]byte(columns), &idx.Columns); err != nil {
			return nil, err
		}
		indexes = append(indexes, idx)
	}
	return indexes, rows.Err()
}
