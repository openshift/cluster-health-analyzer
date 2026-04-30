package tests

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("TLS Configuration", func() {
	var (
		originalArgs []string
		localPort    int
		cancelPF     func()
	)

	// patchAndRollout replaces the deployment args and waits for the rollout.
	// The original args are restored in AfterEach.
	patchAndRollout := func(args []string) {
		By("Patching deployment args")
		Expect(cluster.SetDeploymentArgs(ctx, cfg.DeploymentName, 0, args)).To(Succeed())
		By("Waiting for rollout to complete")
		Expect(cluster.WaitForRollout(ctx, cfg.DeploymentName)).To(Succeed())
	}

	BeforeEach(func() {
		By("Reading current deployment args")
		var err error
		originalArgs, err = cluster.GetDeploymentArgs(ctx, cfg.DeploymentName, 0)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if cancelPF != nil {
			cancelPF()
			cancelPF = nil
		}

		By("Restoring original deployment args")
		Expect(cluster.SetDeploymentArgs(ctx, cfg.DeploymentName, 0, originalArgs)).To(Succeed())
		Expect(cluster.WaitForRollout(ctx, cfg.DeploymentName)).To(Succeed())
	})

	// connectTLS dials the metrics endpoint using the given config.
	// InsecureSkipVerify is always set — the server uses a self-signed cert.
	connectTLS := func(clientCfg *tls.Config) (version uint16, retErr error) {
		clientCfg.InsecureSkipVerify = true //nolint:gosec // self-signed cert in test environment
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", fmt.Sprintf("localhost:%d", localPort), clientCfg)
		if err != nil {
			return 0, err
		}
		defer func() {
			if closeErr := conn.Close(); closeErr != nil && retErr == nil {
				retErr = closeErr
			}
		}()
		return conn.ConnectionState().Version, nil
	}

	setupPortForward := func() {
		By("Setting up port-forward to the CHA metrics service")
		var err error
		localPort, cancelPF, err = cluster.PortForward("svc/"+cfg.DeploymentName, 8443)
		Expect(err).NotTo(HaveOccurred())
	}

	Describe("with --tls-min-version=VersionTLS13", func() {
		BeforeEach(func() {
			patchAndRollout(append(originalArgs, "--tls-min-version=VersionTLS13"))
			setupPortForward()
		})

		It("should negotiate TLS 1.3", func() {
			version, err := connectTLS(&tls.Config{})
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint16(tls.VersionTLS13)),
				"Expected TLS 1.3 but negotiated %s", tls.VersionName(version))
		})

		It("should reject a TLS 1.2-only client", func() {
			_, err := connectTLS(&tls.Config{MaxVersion: tls.VersionTLS12})
			Expect(err).To(HaveOccurred(), "Expected TLS 1.2 connection to be rejected")
		})
	})

	Describe("with --tls-min-version=VersionTLS12 and a specific cipher suite", func() {
		// allowedSuite is configured on the server; rejectedSuite is not.
		const (
			minVersion    = tls.VersionTLS12
			minVersionFlag = "VersionTLS12"
			allowedSuite  = tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
			rejectedSuite = tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
		)

		BeforeEach(func() {
			patchAndRollout(append(originalArgs,
				"--tls-min-version="+minVersionFlag,
				"--tls-cipher-suites="+tls.CipherSuiteName(allowedSuite),
			))
			setupPortForward()
		})

		It("should accept a client using the configured cipher suite", func() {
			version, err := connectTLS(&tls.Config{
				MaxVersion:   minVersion,
				CipherSuites: []uint16{allowedSuite},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint16(tls.VersionTLS12)),
				"Expected TLS 1.2 but negotiated %s", tls.VersionName(version))
		})

		It("should reject a client that offers only a non-configured cipher suite", func() {
			_, err := connectTLS(&tls.Config{
				MaxVersion:   minVersion,
				CipherSuites: []uint16{rejectedSuite},
			})
			Expect(err).To(HaveOccurred(), "Expected connection with non-configured cipher suite to be rejected")
		})

		It("should negotiate TLS 1.3 when the client supports it (cipher suites do not apply to TLS 1.3)", func() {
			version, err := connectTLS(&tls.Config{})
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint16(tls.VersionTLS13)),
				"Expected TLS 1.3 but negotiated %s", tls.VersionName(version))
		})
	})
})
