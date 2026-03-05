package tools

import "testing"

// TestBuiltinExportCompatibility проверяет, что re-export конструкторов совместим с интерфейсом Tool.
func TestBuiltinExportCompatibility(t *testing.T) {
	var _ Tool = NewTimeNowTool()
	var _ Tool = NewHTTPGetTool(HTTPGetConfig{AllowDomains: []string{"example.com"}})

	if NewTimeNowTool() == nil {
		t.Fatalf("time.now constructor returned nil")
	}
	if NewHTTPGetTool(HTTPGetConfig{AllowDomains: []string{"example.com"}}) == nil {
		t.Fatalf("http.get constructor returned nil")
	}
}
