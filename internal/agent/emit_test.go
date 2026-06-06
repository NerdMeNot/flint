package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadEmits_NormalKeyValue(t *testing.T) {
	dir := t.TempDir()
	content := "VERSION=1.2.3\nDEPLOY_TARGET=staging\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".flint-emit"), []byte(content), 0644))

	outputs, err := ReadEmits(dir)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"VERSION":       "1.2.3",
		"DEPLOY_TARGET": "staging",
	}, outputs)
}

func TestReadEmits_ValueWithEqualsSign(t *testing.T) {
	dir := t.TempDir()
	content := "KEY=a=b\nCONNECTION=host=db port=5432\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".flint-emit"), []byte(content), 0644))

	outputs, err := ReadEmits(dir)
	require.NoError(t, err)
	assert.Equal(t, "a=b", outputs["KEY"])
	assert.Equal(t, "host=db port=5432", outputs["CONNECTION"])
}

func TestReadEmits_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".flint-emit"), []byte(""), 0644))

	outputs, err := ReadEmits(dir)
	require.NoError(t, err)
	assert.Empty(t, outputs)
}

func TestReadEmits_MissingFile(t *testing.T) {
	dir := t.TempDir()
	// No .flint-emit file exists.

	outputs, err := ReadEmits(dir)
	require.NoError(t, err)
	assert.Nil(t, outputs)
}

func TestReadEmits_MalformedLinesSkipped(t *testing.T) {
	dir := t.TempDir()
	content := "GOOD=value\nmalformed line no equals\n\nANOTHER=ok\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".flint-emit"), []byte(content), 0644))

	outputs, err := ReadEmits(dir)
	require.NoError(t, err)
	assert.Len(t, outputs, 2)
	assert.Equal(t, "value", outputs["GOOD"])
	assert.Equal(t, "ok", outputs["ANOTHER"])
}

func TestReadEmits_WhitespaceHandling(t *testing.T) {
	dir := t.TempDir()
	content := "  KEY  =  value  \n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".flint-emit"), []byte(content), 0644))

	outputs, err := ReadEmits(dir)
	require.NoError(t, err)
	assert.Equal(t, "value", outputs["KEY"])
}
