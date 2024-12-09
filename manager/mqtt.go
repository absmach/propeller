package manager

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/proplet"
)

const aliveHistoryLimit = 10

func Subscribe(ctx context.Context, channelID string, pubsub mqtt.PubSub, propletsDB storage.Storage, logger *slog.Logger) error {
	baseTopic := "channels/" + channelID + "/messages"
	topic := baseTopic + "/#"

	if err := pubsub.Subscribe(ctx, topic, Handle(ctx, baseTopic, propletsDB, logger)); err != nil {
		return err
	}

	return nil
}

func Handle(ctx context.Context, baseTopic string, propletsDB storage.Storage, logger *slog.Logger) func(topic string, msg map[string]interface{}) error {
	return func(topic string, msg map[string]interface{}) error {
		switch topic {
		case baseTopic + "/control/proplet/create":
			if err := createProplet(ctx, msg, propletsDB); err != nil {
				return err
			}

			logger.InfoContext(ctx, "successfully created proplet")
		case baseTopic + "/control/proplet/alive":
			return updateLiveness(ctx, msg, propletsDB)
		}

		return nil
	}
}

func createProplet(ctx context.Context, msg map[string]interface{}, propletsDB storage.Storage) error {
	propletID, ok := msg["proplet_id"].(string)
	if !ok {
		return errors.New("invalid proplet_id")
	}
	if propletID == "" {
		return errors.New("proplet id is empty")
	}
	name, ok := msg["name"].(string)
	if !ok {
		name = ""
	}

	p := proplet.Proplet{
		ID:   propletID,
		Name: name,
	}
	if err := propletsDB.Create(ctx, p.ID, p); err != nil {
		return err
	}

	return nil
}

func updateLiveness(ctx context.Context, msg map[string]interface{}, propletsDB storage.Storage) error {
	propletID, ok := msg["proplet_id"].(string)
	if !ok {
		return errors.New("invalid proplet_id")
	}
	if propletID == "" {
		return errors.New("proplet id is empty")
	}
	data, err := propletsDB.Get(ctx, propletID)
	if err != nil {
		return err
	}
	p, ok := data.(proplet.Proplet)
	if !ok {
		return errors.New("invalid proplet data")
	}

	p.Alive = true
	p.AliveHistory = append(p.AliveHistory, time.Now())
	if len(p.AliveHistory) > aliveHistoryLimit {
		p.AliveHistory = p.AliveHistory[1:]
	}
	if err := propletsDB.Update(ctx, propletID, p); err != nil {
		return err
	}

	return nil
}
