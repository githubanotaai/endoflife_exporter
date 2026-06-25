package collector

import (
	"testing"

	"github.com/veerendra2/endoflife_exporter/internal/config"
)

func TestCustomMetricLabelNames(t *testing.T) {
	cfg := config.Config{
		Products: []config.Product{
			{
				Name: "kubernetes",
				Labels: map[string]string{
					"cluster":     "aaipltf-use1-prd-cnc",
					"environment": "production",
				},
			},
			{
				Name: "postgres",
				Labels: map[string]string{
					"environment":  "production",
					"owner_team":   "database-platform",
					"service_name": "postgres",
				},
			},
		},
	}

	got, err := customMetricLabelNames(cfg)
	if err != nil {
		t.Fatalf("customMetricLabelNames returned error: %v", err)
	}

	want := []string{"cluster", "environment", "owner_team", "service_name"}
	if len(got) != len(want) {
		t.Fatalf("customMetricLabelNames() = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("customMetricLabelNames() = %v, want %v", got, want)
		}
	}
}

func TestCustomMetricLabelNamesRejectsInvalidLabels(t *testing.T) {
	tests := []struct {
		name      string
		labelName string
	}{
		{
			name:      "invalid prometheus label",
			labelName: "owner-team",
		},
		{
			name:      "reserved prometheus label",
			labelName: "__name__",
		},
		{
			name:      "internal metric label",
			labelName: "product_name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := customMetricLabelNames(config.Config{
				Products: []config.Product{
					{
						Name: "kubernetes",
						Labels: map[string]string{
							tt.labelName: "value",
						},
					},
				},
			})
			if err == nil {
				t.Fatalf("customMetricLabelNames() error = nil, want error")
			}
		})
	}
}
