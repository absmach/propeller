package mqtt

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"

	"github.com/absmach/propeller/proxy"
	"golang.org/x/sync/errgroup"
)

type Proxy struct {
	config  proxy.Config
	handler proxy.Handler
	logger  *slog.Logger
	dialer  net.Dialer
}

func New(config proxy.Config, handler proxy.Handler, logger *slog.Logger) *Proxy {
	return &Proxy{
		config:  config,
		handler: handler,
		logger:  logger,
	}
}

func (p Proxy) accept(ctx context.Context, l net.Listener) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn, err := l.Accept()
			if err != nil {
				p.logger.Warn("Accept error " + err.Error())
				continue
			}
			p.logger.Info("Accepted new client")
			go p.handle(ctx, conn)
		}
	}
}

func (p Proxy) handle(ctx context.Context, inbound net.Conn) {
	defer p.close(inbound)
	outbound, err := p.dialer.Dial("tcp", p.config.Target)
	if err != nil {
		p.logger.Error("Cannot connect to remote broker " + p.config.Target + " due to: " + err.Error())
		return
	}
	defer p.close(outbound)

	if err = proxy.Stream(ctx, inbound, outbound, p.handler); err != io.EOF {
		p.logger.Warn(err.Error())
	}
}

// Listen of the server, this will block.
func (p Proxy) Listen(ctx context.Context) error {
	l, err := net.Listen("tcp", p.config.Address)
	if err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		p.accept(ctx, l)
		return nil
	})

	g.Go(func() error {
		<-ctx.Done()
		return l.Close()
	})
	if err := g.Wait(); err != nil {
		p.logger.Info(fmt.Sprintf("MQTT proxy server at %s", p.config.Address), slog.String("error", err.Error()))
	} 

	return nil
}

func (p Proxy) close(conn net.Conn) {
	if err := conn.Close(); err != nil {
		p.logger.Warn(fmt.Sprintf("Error closing connection %s", err.Error()))
	}
}
