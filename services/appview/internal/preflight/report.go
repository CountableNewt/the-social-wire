package preflight

type Report struct {
	Kind                 string       `json:"kind"`
	DatabaseBytes        int64        `json:"databaseBytes"`
	SchemaSHA256         string       `json:"schemaSha256"`
	Tables               []TableCount `json:"tables"`
	MigrationIdentifiers []string     `json:"migrationIdentifiers"`
}
