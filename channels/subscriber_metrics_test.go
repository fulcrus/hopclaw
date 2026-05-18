package channels

import "testing"

func TestRecordSubscriberDropIncrementsCounter(t *testing.T) {
	t.Parallel()

	before := SubscriberDropCount("test-adapter")
	RecordSubscriberDrop("test-adapter")
	after := SubscriberDropCount("test-adapter")
	if after != before+1 {
		t.Fatalf("SubscriberDropCount() = %d, want %d", after, before+1)
	}
}
