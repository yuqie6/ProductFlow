package localedit

import "testing"

func TestAuditJSONRecordsMeasuredSourceAndMask(t *testing.T) {
	provider := "openai_images"
	mode := "mask_edit"
	snap := snapshot{
		taskRow: taskRow{
			Operation:         "inpaint",
			SourceSHA:         "abc123",
			RequestedProvider: &provider,
			RequestedMode:     &mode,
			ReferenceIDs:      []string{"ref-1"},
		},
		SourceMIME:   "image/png",
		SourceWidth:  1024,
		SourceHeight: 768,
		MaskMIME:     "image/png",
		MaskWidth:    1024,
		MaskHeight:   768,
		MaskSHA:      "masksha",
	}
	got := snap.auditJSON()
	if got["size"] != "1024x768" {
		t.Fatalf("size %+v", got["size"])
	}
	source, _ := got["source"].(map[string]any)
	if source["mime_type"] != "image/png" || source["width"] != 1024 || source["height"] != 768 || source["sha256"] != "abc123" {
		t.Fatalf("source %+v", source)
	}
	mask, _ := got["mask"].(map[string]any)
	if mask["mime_type"] != "image/png" || mask["width"] != 1024 || mask["height"] != 768 || mask["sha256"] != "masksha" {
		t.Fatalf("mask %+v", mask)
	}
}
