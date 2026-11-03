package postgres

import (
	"context"
	"database/sql"

	"github.com/ForgeSchema/forgeschema/schema"
)

// readViews reads the managed schema's plain views. Each definition is
// printed with pg_catalog and then the managed schema on the search path, so
// the schema's own names appear unqualified, exactly as the CREATE VIEW in a
// plan runs them; the catalog-only search path is restored afterwards.
func readViews(ctx context.Context, tx *sql.Tx, nsoid int64, name string) ([]schema.View, error) {
	rows, err := tx.QueryContext(ctx, `SELECT c.oid,c.relname,
 c.reloptions IS NOT NULL OR c.relpersistence<>'p',
 EXISTS(SELECT 1 FROM pg_catalog.pg_trigger t WHERE t.tgrelid=c.oid)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_rewrite r WHERE r.ev_class=c.oid AND r.rulename<>'_RETURN')
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_attrdef ad WHERE ad.adrelid=c.oid)
 OR EXISTS(SELECT 1 FROM pg_catalog.pg_policy p WHERE p.polrelid=c.oid),
 COALESCE((SELECT o.identity FROM pg_catalog.pg_rewrite r JOIN pg_catalog.pg_depend d
  ON d.classid='pg_catalog.pg_rewrite'::regclass AND d.objid=r.oid AND d.deptype='n'
  CROSS JOIN LATERAL pg_catalog.pg_identify_object(d.refclassid,d.refobjid,d.refobjsubid) o
  WHERE r.ev_class=c.oid AND o.schema IS DISTINCT FROM $2
  ORDER BY o.identity LIMIT 1),'')
 FROM pg_catalog.pg_class c WHERE c.relnamespace=$1 AND c.relkind='v' ORDER BY c.relname`, nsoid, name)
	if err != nil {
		return nil, err
	}
	type viewInfo struct {
		oid             int64
		name            string
		options, extras bool
		outside         string
	}
	var infos []viewInfo
	for rows.Next() {
		var v viewInfo
		if err = rows.Scan(&v.oid, &v.name, &v.options, &v.extras, &v.outside); err != nil {
			rows.Close()
			return nil, err
		}
		infos = append(infos, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(infos) == 0 {
		return nil, nil
	}
	for _, v := range infos {
		object := name + "." + v.name
		switch {
		case v.options:
			return nil, unsupported(object, "views with options (check option, security barrier, security invoker) or temporary and unlogged views are not represented")
		case v.extras:
			return nil, unsupported(object, "views with triggers, rules, column defaults or policies are not represented")
		case v.outside != "":
			return nil, unsupported(object, "the view reads "+v.outside+", outside the managed schema")
		}
	}
	if _, err = tx.ExecContext(ctx, "SELECT pg_catalog.set_config('search_path', 'pg_catalog, '||pg_catalog.quote_ident($1), true)", name); err != nil {
		return nil, err
	}
	var out []schema.View
	for _, v := range infos {
		object := name + "." + v.name
		view := schema.View{Name: v.name}
		var def sql.NullString
		if err = tx.QueryRowContext(ctx, "SELECT pg_catalog.pg_get_viewdef($1::oid, true)", v.oid).Scan(&def); err != nil {
			return nil, err
		}
		if view.Query, err = schema.NormalizeViewQuery(def.String); err != nil {
			return nil, unsupported(object, "the stored query can't be represented: "+err.Error())
		}
		deps, err := tx.QueryContext(ctx, `SELECT DISTINCT rc.relname FROM pg_catalog.pg_rewrite r
 JOIN pg_catalog.pg_depend d ON d.classid='pg_catalog.pg_rewrite'::regclass AND d.objid=r.oid AND d.deptype='n'
  AND d.refclassid='pg_catalog.pg_class'::regclass
 JOIN pg_catalog.pg_class rc ON rc.oid=d.refobjid
 WHERE r.ev_class=$1 AND rc.oid<>$1 AND rc.relnamespace=$2 AND rc.relkind IN ('r','v') ORDER BY rc.relname`, v.oid, nsoid)
		if err != nil {
			return nil, err
		}
		for deps.Next() {
			var n string
			if err = deps.Scan(&n); err != nil {
				deps.Close()
				return nil, err
			}
			view.DependsOn = append(view.DependsOn, n)
		}
		err = deps.Err()
		deps.Close()
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	if _, err = tx.ExecContext(ctx, "SELECT pg_catalog.set_config('search_path', 'pg_catalog', true)"); err != nil {
		return nil, err
	}
	return out, nil
}
