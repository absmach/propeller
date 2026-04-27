package api

import (
	"testing"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/task"
	"github.com/stretchr/testify/assert"
)

func TestListEntityReqValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc string
		req  listEntityReq
		err  error
	}{
		{
			desc: "empty status is allowed",
			req: listEntityReq{
				statusFilter: propletStatusFilter,
			},
			err: nil,
		},
		{
			desc: "valid proplet status",
			req: listEntityReq{
				status:       proplet.ActiveStatus.String(),
				statusFilter: propletStatusFilter,
			},
			err: nil,
		},
		{
			desc: "valid job status",
			req: listEntityReq{
				status:       task.RunningStatus.String(),
				statusFilter: jobStatusFilter,
			},
			err: nil,
		},
		{
			desc: "tasks do not support status filtering",
			req: listEntityReq{
				status: task.PendingStatus.String(),
			},
			err: errStatusFilterUnsupported,
		},
		{
			desc: "invalid proplet status",
			req: listEntityReq{
				status:       "mystery",
				statusFilter: propletStatusFilter,
			},
			err: proplet.ErrInvalidStatus,
		},
		{
			desc: "invalid job status",
			req: listEntityReq{
				status:       "mystery",
				statusFilter: jobStatusFilter,
			},
			err: task.ErrInvalidJobStatus,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			err := tc.req.validate()
			assert.Equal(t, tc.err, err, "%s: expected %v got %v", tc.desc, tc.err, err)
		})
	}
}
