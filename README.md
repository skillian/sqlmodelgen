# sqlmodelgen
Generate SQL model classes/structs in various languages (e.g. Go, C#) from a
JSON configuration.

There are two main action types that `sqlmodelgen` performs:  `generator`s
and `target`s.

A `generator` reads some input file and translates it into a `models.json`
database description.  An example `generator` is the `sql-reflector` generator
which connects into an existing database and inspects the tables and
relationships between those tables.

A `target` reads a `models.json` file and generates some output file.  An
example target is `sqlddl-mssql` which generates SQL "Data Description
Language" (i.e. `CREATE DATABASE ...`, `CREATE TABLE ...` statements) for
Microsoft SQL Server.

## Examples

### Generate a `models.json` file from a Draw.io / Diagrams.net diagram

```bash
sqlmodelgen -g drawio "models.json" "MyDiagram.drawio.xml"
```

#### Note

Note that the Draw.io/Diagrams.net diagram has to be exported as XML with the
"Compressed" checkbox unchecked.

### Generate a SQL DDL script for Microsoft SQL Server

```bash
sqlmodelgen -t sqlddl-mssql "MyDatabase.sql" -p 0 namespace "see note below" "models.json"
```

#### Note

Originally, the intended targets were all intended to be source code like
C#, Go, Python, etc..  All of those languages have ways of defining code in
namespaces (actual `namespace`s in C#, or modules or packages in both Go and
Python).  Because of that, the `namespace` parameter (`-p` above) is required.

### Generate SQL DDL, Go SQL and Domain models all at once

```bash
sqlmodelgen \
	-t sqlddl-mssql "create-schema.sql" \
	-t go-sql "sqldata/models.go" \
	-t go-models "domain/models.go" \
	-p 0 namespace "ignored" \
	-p 1 namespace "sqldata" \
	-p 2 namespace "domain"
```

This helps (or tries to) demonstrate what the number after the `-p` option
is for:  It specifies the index of the `target` or `generator` to which the
parameter should apply.  `"ignored"` is the `namespace` for index 0:
sqlddl-msslq.  `"sqldata"` is the `namespace` for index 1: go-sql, etc.
