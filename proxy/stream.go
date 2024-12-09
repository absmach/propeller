package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	orasHTTP "github.com/absmach/propeller/proxy/http"
	"github.com/eclipse/paho.mqtt.golang/packets"
)

func Stream(ctx context.Context, in, out net.Conn, h Handler) error {
	errs := make(chan error, 2)

	go streamHTTP(ctx, in, out, h, errs)
	go streamMQTT(ctx, out, in, h, errs)

	err := <-errs

	disconnectErr := h.Disconnect(ctx)

	return errors.Join(err, disconnectErr)
}

func streamHTTP(ctx context.Context, _, w net.Conn, h Handler, errs chan error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			errs <- ctx.Err()
			return
		default:
			data, err := orasHTTP.FetchFromOCI(ctx)
			if err != nil {
				errs <- err
				return
			}

			if err = h.Connect(ctx); err != nil {
				errs <- err
				return
			}

			if _, err = w.Write(data); err != nil {
				errs <- err
				return
			}
		}
	}
}

func streamMQTT(ctx context.Context, r, w net.Conn, h Handler, errs chan error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			errs <- ctx.Err()
		default:
			pkt, err := packets.ReadPacket(r)
			if err != nil {
				errs <- err
				return
			}

			switch p := pkt.(type) {
			case *packets.PublishPacket:
				topics := p.TopicName
				if err = h.Publish(ctx, &topics, &p.Payload); err != nil {
					disconnectPkt := packets.NewControlPacket(packets.Disconnect).(*packets.DisconnectPacket)
					if wErr := disconnectPkt.Write(w); wErr != nil {
						err = errors.Join(err, wErr)
					}
					errs <- fmt.Errorf("MQTT publish error: %w", err)
					return
				}

			case *packets.ConnectPacket:
				if err = h.Connect(ctx); err != nil {
					errs <- fmt.Errorf("MQTT connection error: %w", err)
					return
				}

			case *packets.DisconnectPacket:
				errs <- h.Disconnect(ctx)
				return
			}

			if err := pkt.Write(w); err != nil {
				errs <- err
				return
			}
		}

	}
}
