package main

import (
	"bytes"
	"testing"
)

func TestMigrationCommandRejectsMissingGuardsWithoutExposingEnvironment(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"--receipt", t.TempDir() + "/receipt.json"}, []string{"BLOB_READ_WRITE_TOKEN=private-credential", "SOCIAL_DRAFT_S3_SECRET_ACCESS_KEY=private-source-credential"}, &stdout, &stderr)
	if code != 1 || stdout.Len() != 0 || bytes.Contains(stderr.Bytes(), []byte("credential")) {
		t.Fatal("unguarded migration exposed environment", code, stderr.String())
	}
}

func TestMigrationCommandUsageDoesNotEchoArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{"--unexpected-secret"}, nil, &stdout, &stderr); code != 2 || bytes.Contains(stderr.Bytes(), []byte("unexpected-secret")) {
		t.Fatal("argument diagnostic", code, stderr.String())
	}
}
