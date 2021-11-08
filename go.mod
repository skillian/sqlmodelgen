module github.com/skillian/sqlmodelgen

go 1.16

replace github.com/skillian/argparse => ../argparse

replace github.com/skillian/expr => ../expr

replace github.com/skillian/logging => ../logging

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/denisenkom/go-mssqldb v0.10.0
	github.com/skillian/argparse v0.0.0-00010101000000-000000000000
	github.com/skillian/expr v0.0.0-00010101000000-000000000000
	github.com/skillian/logging v0.0.0-20210425124543-4b3b9b919a80
	github.com/skillian/textwrap v0.0.0-20190707153458-15c7ee8d44ed
	github.com/xuri/excelize/v2 v2.4.1
)
