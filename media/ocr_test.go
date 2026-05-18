package media

import "testing"

func TestDecodeOCRResponseParsesStructuredJSON(t *testing.T) {
	t.Parallel()

	result := decodeOCRResponse(`{
  "text": "Invoice 42",
  "confidence": 0.97,
  "language_detected": "en",
  "regions": [
    {"text": "Invoice", "confidence": 0.99, "x": 10, "y": 12, "width": 50, "height": 18},
    {"text": "42", "confidence": 0.95, "x": 70, "y": 12, "width": 16, "height": 18}
  ]
}`, OCRConfig{OutputFormat: OCRFormatJSON})

	if result.Text != "Invoice 42" {
		t.Fatalf("Text = %q, want %q", result.Text, "Invoice 42")
	}
	if result.LanguageDetected != "en" {
		t.Fatalf("LanguageDetected = %q, want %q", result.LanguageDetected, "en")
	}
	if result.Confidence != 0.97 {
		t.Fatalf("Confidence = %v, want %v", result.Confidence, 0.97)
	}
	if len(result.Regions) != 2 {
		t.Fatalf("len(Regions) = %d, want 2", len(result.Regions))
	}
	if result.Regions[0].Text != "Invoice" || result.Regions[1].Text != "42" {
		t.Fatalf("Regions = %#v, want parsed region text", result.Regions)
	}
}

func TestDecodeOCRResponseFallsBackToRawText(t *testing.T) {
	t.Parallel()

	result := decodeOCRResponse("plain OCR text", OCRConfig{OutputFormat: OCRFormatJSON})
	if result.Text != "plain OCR text" {
		t.Fatalf("Text = %q, want raw text fallback", result.Text)
	}
	if len(result.Regions) != 0 {
		t.Fatalf("Regions = %#v, want none on raw text fallback", result.Regions)
	}
}
