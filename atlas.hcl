data "external_schema" "gorm" {
  program = [
    "go",
    "run",
    "-tags=tools",
    "./tools/atlas/load.go",
    "postgres",
  ]
}

env "gorm" {
  src = data.external_schema.gorm.url
  dev = "docker://postgres/17/dev"
  migration {
    dir = "file://pkg/storage/migrations"
  }
  format {
    migrate {
      diff = "{{ sql . \"  \" }}"
    }
  }
}
