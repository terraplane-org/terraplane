//go:build tools

package main

import (
	"fmt"
	"io"
	"os"

	"ariga.io/atlas-provider-gorm/gormschema"
	"github.com/xyzjace/terraplane/pkg/storage/models"
)

func main() {
	dialect := "postgres"
	if len(os.Args) > 1 && os.Args[1] != "" {
		dialect = os.Args[1]
	}

	loader := gormschema.New(dialect)
	stmts, err := loader.Load(
		&models.ProjectLock{},
		&models.Job{},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load gorm schema: %v\n", err)
		os.Exit(1)
	}

	if _, err := io.WriteString(os.Stdout, stmts); err != nil {
		fmt.Fprintf(os.Stderr, "write schema: %v\n", err)
		os.Exit(1)
	}
}
