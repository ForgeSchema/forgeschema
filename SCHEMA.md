# Schema contract, version 1

A document contains required version: 1, a schema name and a tables sequence. Use tables: [] for an empty managed schema. A table has a name, a nonempty columns sequence and optional primary_key, unique, indexes and foreign_keys. [examples/shop.yaml](examples/shop.yaml) uses every part of the contract.

Names are lower-case ASCII letters, digits and underscores; they must start with a letter or underscore and fit PostgreSQL's 63-byte limit. Names beginning pg_ or forgeschema_ and information_schema are reserved. The inspector uses the same naming policy; identifiers outside it are unsupported, not silently renamed. At most 1600 columns per table and 32 columns per key, constraint or index are accepted.

## Columns

A column has name, type and optional nullable, default and identity. Omitted nullable means false (NOT NULL).

Types: smallint, integer, bigint, text, varchar(n), varchar, char(n), numeric(p,s), numeric(p), numeric, boolean, uuid, date, timestamp without time zone, timestamp with time zone, jsonb, bytea, real and double precision. Aliases int2, int/int4, int8, bool, timestamp, timestamptz, float4, float8, character varying, character, char and decimal normalize to PostgreSQL's own spelling, for example varchar(20) becomes character varying(20), char becomes character(1) and numeric(5) becomes numeric(5,0). Type spelling is case-insensitive and whitespace is normalized. Lengths run from 1 to 10485760; numeric precision from 1 to 1000 with a scale from 0 to the precision.

A default is a YAML string, because the YAML reader never coerces scalars. Only forms that PostgreSQL stores and reports back unchanged are accepted, so that a default survives inspect, plan, apply and verify exactly:

| Column type | Accepted defaults (YAML) |
| --- | --- |
| smallint, integer, bigint | an integer literal in range: `"0"`, `"-1"` |
| numeric, real, double precision | a decimal literal: `"0"`, `"-1.25"` |
| boolean | `"true"` or `"false"` |
| text, varchar, char | a single-quoted literal with '' for a quote: `"'active'"`, `"'it''s'"`, `"''"` |
| jsonb | `"'{}'"` or `"'[]'"` |
| timestamp with time zone | `now()`, `current_timestamp` |
| timestamp without time zone | `now()`, `current_timestamp`, `localtimestamp` |
| date | `current_date` |
| uuid | `gen_random_uuid()` |

Function names are case-insensitive and stored in lower case. Numeric literals must fit the column exactly: no more fractional digits than a numeric scale (rounding would change the value) and no more whole digits than precision minus scale. Negative zero, leading zeros, exponents, backslashes in string literals, string literals longer than a varchar/char length, expressions, casts and other functions are rejected. Backslashes are excluded because a server running with standard_conforming_strings off would read them as escapes. bytea has no supported default.

identity is always or by_default and maps to GENERATED ALWAYS / BY DEFAULT AS IDENTITY. It needs a smallint, integer or bigint column that is not nullable and has no default. PostgreSQL creates the sequence TABLE_COLUMN_seq with default options; that name is reserved and must fit 63 bytes. serial columns are not supported; use identity.

## Keys, constraints and indexes

primary_key lists nonnullable columns in order. Its constraint and index are always named TABLE_pkey.

unique is a sequence of named UNIQUE constraints, each with ordered columns. The backing index shares the constraint name. Nullable columns are allowed; NULLS NOT DISTINCT is not.

indexes is a sequence of named btree indexes with ordered columns and optional unique: true for a unique index that is not a constraint.

foreign_keys is a sequence of named constraints with columns, references (table and columns in the same schema) and optional on_delete and on_update: no_action (the default), restrict, cascade, set_null or set_default. The referenced columns, in any order, must be the referenced table's primary key, a unique constraint or a unique index. Paired columns must have the same type or belong to the same family: integers, varchar/text, char or numeric (any precision). set_null needs nullable columns. Self-references are allowed.

Primary key, unique constraint, index and identity sequence names share PostgreSQL's per-schema relation namespace and must not collide with each other or with tables. Foreign key names must be unique within their table and differ from its primary key and unique constraint names.

## Strict reading and canonical form

The YAML reader rejects unknown and duplicate fields, null values, multiple documents, custom tags, anchors, aliases and scalar coercion (quote-looking booleans and numbers do not become booleans or numbers automatically). The input limit is 1 MiB. Documents constructed directly in Go are validated too. Canonical JSON sorts tables, columns, constraints and indexes by name but preserves column order inside keys, constraints and indexes. no_action actions are omitted. Physical column order is outside this contract. SHA-256 fingerprints cover canonical JSON, including the contract version. Fingerprints detect representation differences; they are not signatures or plan authorization. Documents that use none of the newer fields have the same canonical JSON and fingerprint as in 0.1.

Explicit rename intent is supplied through the separate versioned mapping file described in PLANNING.md; schema files themselves have no rename field. Read-only PostgreSQL 16/17 inspection maps supported catalog objects to this contract and rejects unsupported structures; see INSPECTION.md. Offline SQL generation is available through the planner. Supported reviewed plans can be executed transactionally through apply; see APPLY.md.

Not represented: generated columns, timestamp/time precision, arrays, enums, domains, custom collations, check and exclusion constraints, deferrable constraints, cross-schema foreign keys, MATCH FULL, partial, expression, INCLUDE and non-btree indexes, and index options.
