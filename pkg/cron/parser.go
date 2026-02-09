package cron

import (
	"errors"
	"time"

	"github.com/robfig/cron/v3"
)

var ErrInvalidCronExpression = errors.New("invalid cron expression")

type CronSchedule struct {
	parser cron.Parser
	spec   cron.Schedule
}

func ParseCronExpression(expr string) (*CronSchedule, error) {
	if expr == "" {
		return nil, ErrInvalidCronExpression
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	spec, err := parser.Parse(expr)
	if err != nil {
		return nil, ErrInvalidCronExpression
	}

	return &CronSchedule{
		parser: parser,
		spec:   spec,
	}, nil
}

func ValidateCronExpression(expr string) error {
	_, err := ParseCronExpression(expr)

	return err
}

func CalculateNextRun(schedule *CronSchedule, from time.Time, timezone string) time.Time {
	if schedule == nil || schedule.spec == nil {
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
	nextRun := schedule.spec.Next(localTime)

	return nextRun
}
