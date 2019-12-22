package serve

// FIXME[P4]: may be move this to github.com/IPA-CyberLab/kmgm/srv ?

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_auth "github.com/grpc-ecosystem/go-grpc-middleware/auth"
	grpc_zap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"github.com/grpc-ecosystem/go-grpc-middleware/util/metautils"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"

	"github.com/IPA-CyberLab/kmgm/cli"
	"github.com/IPA-CyberLab/kmgm/cli/serve/authprofile"
	"github.com/IPA-CyberLab/kmgm/cli/serve/certificateservice"
	"github.com/IPA-CyberLab/kmgm/cli/serve/httpzaplog"
	"github.com/IPA-CyberLab/kmgm/cli/serve/issuehandler"
	"github.com/IPA-CyberLab/kmgm/pb"
	"github.com/IPA-CyberLab/kmgm/remote/user"
	"github.com/IPA-CyberLab/kmgm/san"
	"github.com/IPA-CyberLab/kmgm/storage"
)

func grpcHttpMux(grpcServer *grpc.Server, httpHandler http.Handler) http.Handler {
	// based on code from:
	// https://github.com/philips/grpc-gateway-example

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			grpcServer.ServeHTTP(w, r)
		} else {
			httpHandler.ServeHTTP(w, r)
		}
	})
}

const BearerPrefix = "bearer "

func generateAuthFunc(authp *storage.Profile, bootstrap string) (grpc_auth.AuthFunc, error) {
	cacert, err := authp.ReadCACertificate()
	if err != nil {
		return nil, err
	}

	cp := x509.NewCertPool()
	cp.AddCert(cacert)

	authfunc := func(ctx context.Context) (context.Context, error) {
		u := user.Anonymous

		if p, ok := peer.FromContext(ctx); ok {
			if ti, ok := p.AuthInfo.(credentials.TLSInfo); ok {
				pcerts := ti.State.PeerCertificates
				if len(pcerts) > 0 {
					pc := pcerts[0]
					// FIXME: move this to wcrypto/cert
					if _, err := pc.Verify(x509.VerifyOptions{
						Roots:     cp,
						KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
					}); err != nil {
						return nil, grpc.Errorf(codes.Unauthenticated, "Failed to verify client cert: %v", err)
					}

					u = user.ClientCert(pc.Subject.CommonName)
				}
			}
		}
		if u.Type == pb.AuthenticationType_ANONYMOUS {
			authHeader := metautils.ExtractIncoming(ctx).Get("authorization")

			if authHeader != "" {
				if len(authHeader) < len(BearerPrefix) ||
					!strings.EqualFold(authHeader[:len(BearerPrefix)], BearerPrefix) {
					return nil, grpc.Errorf(codes.Unauthenticated, "Bad scheme")
				}
				token := authHeader[len(BearerPrefix):]

				if bootstrap == "" || token != bootstrap {
					return nil, grpc.Errorf(codes.Unauthenticated, "Bad token")
				}
				u = user.BootstrapToken
			}
		}

		grpc_ctxtags.Extract(ctx).Set("auth.sub", u.Name)
		ctx = user.NewContext(ctx, u)
		return ctx, nil
	}
	return authfunc, nil
}

type Server struct {
	closeC chan error
	errC   chan error
}

func (s *Server) Wait() error {
	return <-s.errC
}

func (s *Server) Close(err error) error {
	s.closeC <- err
	return s.Wait()
}

func StartServer(env *cli.Environment, cfg *Config) (*Server, error) {
	slog := env.Logger.Sugar()

	srv := &Server{
		closeC: make(chan error),
		errC:   make(chan error),
	}

	if cfg.Names.Empty() {
		cfg.Names = san.ForThisHost(cfg.ListenAddr)
	}

	authp, err := authprofile.Ensure(env)
	if err != nil {
		return nil, err
	}

	tlscert, pubkeyhash, err := ensureServerCert(env, authp, cfg.Names)
	if err != nil {
		return nil, err
	}

	authfunc, err := generateAuthFunc(authp, cfg.Bootstrap)
	if err != nil {
		return nil, err
	}

	listenAddr := cfg.ListenAddr
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("Failed to listen %q: %w", listenAddr, err)
	}
	slog.Infof("Started to listen at %q. My public key hash is %s.", listenAddr, pubkeyhash)
	if cfg.Bootstrap != "" {
		slog.Infof("Node bootstrap enabled. Token: %s", cfg.Bootstrap)
		// FIXME[P2]: pick random listenaddr. otherwise this will present "--server :34680" which is useless
		slog.Infof("For your convenience, bootstrap command-line to be executed on your clients would look like: kmgm client --server %s --pinnedpubkey %s --token %s bootstrap", listenAddr, pubkeyhash, cfg.Bootstrap)
	}

	uics := []grpc.UnaryServerInterceptor{
		grpc_ctxtags.UnaryServerInterceptor(
			grpc_ctxtags.WithFieldExtractor(grpc_ctxtags.TagBasedRequestFieldExtractor("log_fields")),
		),
		grpc_auth.UnaryServerInterceptor(authfunc),
		grpc_zap.UnaryServerInterceptor(env.Logger),
		grpc_prometheus.UnaryServerInterceptor,
	}
	grpcServer := grpc.NewServer(
		grpc.Creds(credentials.NewServerTLSFromCert(tlscert)),
		grpc_middleware.WithUnaryServerChain(uics...),
	)
	pb.RegisterHelloServiceServer(grpcServer, &helloService{})
	pb.RegisterVersionServiceServer(grpcServer, &versionService{})
	certsvc, err := certificateservice.New(env)
	if err != nil {
		return nil, err
	}
	pb.RegisterCertificateServiceServer(grpcServer, certsvc)
	reflection.Register(grpcServer)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok\n"))
	})
	mux.Handle("/metrics", promhttp.Handler())
	if cfg.IssueHttp > 0 {
		issueH, err := issuehandler.New(env, cfg.IssueHttp)
		if err != nil {
			return nil, err
		}

		curlcmd, err := issueH.CurlString(listenAddr, pubkeyhash)
		if err != nil {
			return nil, err
		}
		mux.Handle("/issue", issueH)
		slog.Infof("HTTP issue endpoint enabled for %d times.", cfg.IssueHttp)
		slog.Infof("  On clients, exec: %s", curlcmd)
	}

	httpHandler := httpzaplog.Handler{
		Upstream: mux,
		Logger:   env.Logger,
	}

	httpsrv := &http.Server{
		Addr:    listenAddr,
		Handler: grpcHttpMux(grpcServer, httpHandler),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{*tlscert},
			NextProtos:   []string{"h2"},
			ClientAuth:   tls.RequestClientCert,
		},
	}

	closed := false
	var userErr error
	go func() {
		userErr = <-srv.closeC

		if !closed {
			closed = true
			lis.Close()
		}
	}()
	go func() {
		slog.Infof("Starting to accept new conn.")
		err := httpsrv.Serve(tls.NewListener(lis, httpsrv.TLSConfig))
		// Suppress "use of closed network connection" error if we intentionally closed the listener.
		if err != nil && closed {
			srv.errC <- userErr
			close(srv.errC)
			close(srv.closeC)
			return
		}
		srv.errC <- err
		close(srv.errC)
		close(srv.closeC)
	}()

	if cfg.AutoShutdown > 0 {
		slog.Infof("Will start auto-shutdown after %v", cfg.AutoShutdown)
		time.AfterFunc(cfg.AutoShutdown, func() {
			slog.Infof("Starting auto-shutdown since %v passed", cfg.AutoShutdown)
			srv.Close(nil)
		})
	}

	return srv, nil
}

func Run(ctx context.Context, env *cli.Environment, cfg *Config) error {
	s, err := StartServer(env, cfg)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		s.closeC <- nil
	}()
	return s.Wait()
}