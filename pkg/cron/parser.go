package cron

import (
	"errors"
	"time"

	"github.com/robfig/cron/v3"
)

var ErrInvalidCronExpression = errors.New("invalid cron expression")

// Schedule represents a parsed cron schedule that can compute the next run time.
type Schedule interface {
	Next(time.Time) time.Time
}

type cronSchedule struct {
	spec cron.Schedule
}

func (cs *cronSchedule) Next(t time.Time) time.Time {
	return cs.spec.Next(t)
}

func ParseCronExpression(expr string) (Schedule, error) {
	if expr == "" {
		return nil, ErrInvalidCronExpression
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	spec, err := parser.Parse(expr)
	if err != nil {
		return nil, ErrInvalidCronExpression
	}

	return &cronSchedule{
		spec: spec,
	}, nil
}

func ValidateCronExpression(expr string) error {
	_, err := ParseCronExpression(expr)

	return err
}

func CalculateNextRun(schedule Schedule, from time.Time, timezone string) time.Time {
	if schedule == nil {
		return time.Time{}
	}

	loc := time.UTC
	if timezone != "" {
		var err error
		loc, err = time.LoadLocation(timezone)
		if err != nil {
			loc = time.UTC
		}
	}

	localTime := from.In(loc)

	return schedule.Next(localTime)
}
