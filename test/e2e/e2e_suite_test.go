//go:build e2e
// +build e2e

// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/versioneer-tech/lakefs-oss-contrib/test/utils"
)

var (
	// operatorImage is the operator image to be built and loaded for testing.
	operatorImage = "example.com/lakefs-oss-contrib-operator:v0.0.1"
	// authServerImage is the auth server image to be built and loaded for testing.
	authServerImage = "example.com/lakefs-oss-contrib-auth-server:v0.0.1"
)

// TestE2E runs the e2e test suite to validate the solution in an isolated environment.
// The default setup requires Kind.
//
// To enable kubectl kuberc (use custom kubectl configurations), set: KUBECTL_KUBERC=true
// By default, kuberc is disabled to ensure consistent test behavior across different environments.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting lakefs-oss-contrib e2e test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("building the operator image")
	cmd := exec.Command("make", "docker-build-operator", fmt.Sprintf("IMG=%s", operatorImage))
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the operator image")

	By("building the auth server image")
	cmd = exec.Command("make", "docker-build-authserver", fmt.Sprintf("AUTHSERVER_IMG=%s", authServerImage))
	_, err = utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the auth server image")

	By("loading the operator image on Kind")
	err = utils.LoadImageToKindClusterWithName(operatorImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the operator image into Kind")

	By("loading the auth server image on Kind")
	err = utils.LoadImageToKindClusterWithName(authServerImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the auth server image into Kind")

	configureKubectlKubeRC()
})

// Disable kubectl kuberc by default for test isolation.
// This prevents local kubectl configurations from affecting test behavior.
// To enable kuberc, set: KUBECTL_KUBERC=true
func configureKubectlKubeRC() {
	if os.Getenv("KUBECTL_KUBERC") != "true" {
		By("disabling kubectl kuberc for test isolation")
		err := os.Setenv("KUBECTL_KUBERC", "false")
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to disable kubectl kuberc")
		_, _ = fmt.Fprintf(GinkgoWriter,
			"kubectl kuberc disabled for consistent test behavior (override with KUBECTL_KUBERC=true)\n")
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "kubectl kuberc enabled (KUBECTL_KUBERC=true)\n")
	}
}
