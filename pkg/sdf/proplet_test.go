package sdf_test

import (
	"strings"
	"testing"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/sdf"
	"github.com/stretchr/testify/assert"
)

func TestPropletDocument(t *testing.T) {
	cases := []struct {
		desc            string
		proplet         proplet.Proplet
		wantTitleHasID  bool
		wantProperties  []string
		wantActions     []string
		wantEvents      []string
		wantSdfDataKeys []string
	}{
		{
			desc:           "full proplet with name and id",
			proplet:        proplet.Proplet{ID: "abc-123", Name: "my-proplet"},
			wantTitleHasID: true,
			wantProperties: []string{"id", "name", "alive", "task_count", "metadata"},
			wantActions:    []string{"start_task", "stop_task"},
			wantEvents:     []string{"heartbeat", "task_result"},
			wantSdfDataKeys: []string{"PropletMetadata", "TaskDispatch", "HeartbeatPayload", "TaskResult"},
		},
		{
			desc:           "proplet with empty name",
			proplet:        proplet.Proplet{ID: "xyz-456", Name: ""},
			wantTitleHasID: true,
			wantProperties: []string{"id", "name", "alive", "task_count", "metadata"},
			wantActions:    []string{"start_task", "stop_task"},
			wantEvents:     []string{"heartbeat", "task_result"},
			wantSdfDataKeys: []string{"PropletMetadata", "TaskDispatch", "HeartbeatPayload", "TaskResult"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			doc := sdf.PropletDocument(tc.proplet)

			assert.NotEmpty(t, doc.Info.Title)
			assert.NotEmpty(t, doc.Info.Version)

			if tc.wantTitleHasID {
				assert.True(t, strings.Contains(doc.Info.Title, tc.proplet.ID), "expected title to contain proplet ID %q, got %q", tc.proplet.ID, doc.Info.Title)
			}

			thing, ok := doc.SdfThing["Proplet"]
			assert.True(t, ok, "expected SdfThing['Proplet'] to exist")

			for _, key := range tc.wantProperties {
				_, ok := thing.SdfProperty[key]
				assert.True(t, ok, "expected sdfProperty[%q] to exist", key)
			}

			for _, key := range tc.wantActions {
				_, ok := thing.SdfAction[key]
				assert.True(t, ok, "expected sdfAction[%q] to exist", key)
			}

			for _, key := range tc.wantEvents {
				_, ok := thing.SdfEvent[key]
				assert.True(t, ok, "expected sdfEvent[%q] to exist", key)
			}

			for _, key := range tc.wantSdfDataKeys {
				_, ok := thing.SdfData[key]
				assert.True(t, ok, "expected sdfData[%q] to exist", key)
			}
		})
	}
}

func TestPropletDocumentTaskResultStateEnum(t *testing.T) {
	cases := []struct {
		desc        string
		proplet     proplet.Proplet
		wantStates  []string
	}{
		{
			desc:       "task result state enum contains all expected values",
			proplet:    proplet.Proplet{ID: "abc-123", Name: "my-proplet"},
			wantStates: []string{"Completed", "Failed", "Skipped", "Interrupted"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			doc := sdf.PropletDocument(tc.proplet)
			thing := doc.SdfThing["Proplet"]

			result, ok := thing.SdfData["TaskResult"]
			assert.True(t, ok, "expected sdfData['TaskResult'] to exist")

			stateAfford, ok := result.Properties["state"]
			assert.True(t, ok, "expected TaskResult.properties.state to exist")

			enumSet := make(map[string]bool)
			for _, v := range stateAfford.Enum {
				s, ok := v.(string)
				assert.True(t, ok, "expected state enum value to be string, got %T", v)
				enumSet[s] = true
			}

			for _, s := range tc.wantStates {
				assert.True(t, enumSet[s], "missing state enum value %q", s)
			}
		})
	}
}

func TestPropletDocumentMinimumPointerNotAliased(t *testing.T) {
	cases := []struct {
		desc    string
		proplet proplet.Proplet
	}{
		{
			desc:    "minimum pointers are not aliased across fields",
			proplet: proplet.Proplet{ID: "x", Name: "n"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			doc := sdf.PropletDocument(tc.proplet)
			thing := doc.SdfThing["Proplet"]

			taskCount := thing.SdfProperty["task_count"]
			heartbeat := thing.SdfData["HeartbeatPayload"]

			assert.NotNil(t, taskCount.Minimum)
			assert.NotNil(t, heartbeat.Properties["task_count"].Minimum)
			assert.NotSame(t, taskCount.Minimum, heartbeat.Properties["task_count"].Minimum, "Minimum pointers must not be aliased")
		})
	}
}
