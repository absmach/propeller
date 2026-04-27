package proplet_test

import (
	"testing"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/stretchr/testify/assert"
)

func TestStatusString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc   string
		status proplet.Status
		value  string
	}{
		{
			desc:   "active status",
			status: proplet.ActiveStatus,
			value:  proplet.Active,
		},
		{
			desc:   "inactive status",
			status: proplet.InactiveStatus,
			value:  proplet.Inactive,
		},
		{
			desc:   "unknown status",
			status: proplet.Status(100),
			value:  "unknown",
		},
	}

	for _, tc := range cases {
		got := tc.status.String()
		assert.Equal(t, tc.value, got, "Status.String() = %v, expected %v", got, tc.value)
	}
}

func TestToStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc   string
		value  string
		status proplet.Status
		err    error
	}{
		{
			desc:   "active status",
			value:  proplet.Active,
			status: proplet.ActiveStatus,
			err:    nil,
		},
		{
			desc:   "inactive status",
			value:  proplet.Inactive,
			status: proplet.InactiveStatus,
			err:    nil,
		},
		{
			desc:   "invalid status",
			value:  "unknown",
			status: proplet.Status(0),
			err:    proplet.ErrInvalidStatus,
		},
	}

	for _, tc := range cases {
		got, err := proplet.ToStatus(tc.value)
		assert.Equal(t, tc.err, err, "ToStatus() error = %v, expected %v", err, tc.err)
		assert.Equal(t, tc.status, got, "ToStatus() = %v, expected %v", got, tc.status)
	}
}
