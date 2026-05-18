package repl

import "testing"

func TestStreamTextFilterRemovesDSMLFunctionCallBlocksAcrossDeltas(t *testing.T) {
	var filter streamTextFilter

	got := filter.Filter("before\n<｜DSML｜function")
	got += filter.Filter("_calls>\n<｜DSML｜invoke name=\"exec_x2E_run\">")
	got += filter.Filter("\n<｜DSML｜parameter name=\"command\">go</｜DSML｜parameter>\n")
	got += filter.Filter("</｜DSML｜invoke>\n</｜DSML｜function_calls>\nafter\n")
	got += filter.Flush()

	if got != "before\nafter\n" {
		t.Fatalf("filtered stream = %q, want %q", got, "before\nafter\n")
	}
}

func TestStreamTextFilterLeavesRegularTextUntouched(t *testing.T) {
	var filter streamTextFilter

	got := filter.Filter("Hello")
	got += filter.Filter(" world")
	got += filter.Filter("\n")
	got += filter.Flush()

	if got != "Hello world\n" {
		t.Fatalf("filtered stream = %q, want %q", got, "Hello world\n")
	}
}
