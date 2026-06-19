package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type hauntRegressionCase struct {
	name       string
	sourceFile string
	testFunc   string
	deferred   bool
}

func TestHauntRegressionCoverage(t *testing.T) {
	tests := []hauntRegressionCase{
		{
			name:     "conversation display never wakes",
			deferred: true,
		},
		{
			name:     "conversation human cursors are not agent cursors",
			deferred: true,
		},
		{
			name:       "observation historical rows display but never execute",
			sourceFile: filepath.Join("..", "observation", "internal", "service_test.go"),
			testFunc:   "TestLaneMarksStaleRuntimeBreadcrumbDisplayOnly",
		},
		{
			name:       "observation projection is not authority",
			sourceFile: filepath.Join("..", "observation", "internal", "service_test.go"),
			testFunc:   "TestProjectionDoesNotDriveExecution",
		},
		{
			name:       "delivery terminal intent cannot execute again",
			sourceFile: filepath.Join("..", "delivery", "internal", "service_test.go"),
			testFunc:   "TestTerminalIntentCannotExecuteAgain",
		},
		{
			name:       "delivery expired intent cannot be claimed",
			sourceFile: filepath.Join("..", "delivery", "internal", "service_test.go"),
			testFunc:   "TestExpiredIntentCannotBeClaimed",
		},
		{
			name:       "delivery stale runtime cannot claim",
			sourceFile: filepath.Join("..", "delivery", "internal", "service_test.go"),
			testFunc:   "TestStaleRuntimeCannotClaimFreshWork",
		},
		{
			name:       "delivery duplicate claim is rejected",
			sourceFile: filepath.Join("..", "delivery", "internal", "service_test.go"),
			testFunc:   "TestConcurrentClaimOnlyOneSucceeds",
		},
		{
			name:       "delivery legacy import cannot execute",
			sourceFile: filepath.Join("..", "delivery", "internal", "service_test.go"),
			testFunc:   "TestDisplayOnlyIntentCannotExecute",
		},
		{
			name:       "runtime reconnect does not claim stale work",
			sourceFile: filepath.Join("..", "delivery", "internal", "service_test.go"),
			testFunc:   "TestReconnectCannotClaimOldInstanceWork",
		},
		{
			name:       "runtime left membership is not dead runtime",
			sourceFile: filepath.Join("..", "runtime", "internal", "service_test.go"),
			testFunc:   "TestLeftMembershipDoesNotAffectRuntimeState",
		},
		{
			name:       "cursor replay cannot induce action",
			sourceFile: filepath.Join("..", "runtime", "internal", "service_test.go"),
			testFunc:   "TestCursorReplayCannotMoveSubscriptionBackward",
		},
		{
			name:       "watermark skips display-only safely",
			sourceFile: filepath.Join("..", "delivery", "internal", "service_test.go"),
			testFunc:   "TestCutoverWatermarkDisplayOnlyIntentCannotExecute",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.deferred {
				t.Skip("Wave 3 conversation deferred; TODO task 2774")
			}
			assertTestFunctionExists(t, tt.sourceFile, tt.testFunc)
		})
	}
}

func assertTestFunctionExists(t *testing.T, sourceFile string, testFunc string) {
	t.Helper()
	if sourceFile == "" || testFunc == "" {
		t.Fatal("implemented haunt-regression case must name a source file and test function")
	}
	content, err := os.ReadFile(sourceFile)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", sourceFile, err)
	}
	needle := "func " + testFunc + "("
	if !strings.Contains(string(content), needle) {
		t.Fatalf("%s does not define %s", sourceFile, testFunc)
	}
}
