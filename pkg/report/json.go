package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func WriteJSON(dir string, rpt *RunReport) error {
	data, err := json.MarshalIndent(rpt, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "report.json"), data, 0644)
}

func WriteJobFile(dir string, r JobResult) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal job result: %w", err)
	}
	filename := fmt.Sprintf("%s-%s.json", r.Pod, r.Job)
	return os.WriteFile(filepath.Join(dir, filename), data, 0644)
}
