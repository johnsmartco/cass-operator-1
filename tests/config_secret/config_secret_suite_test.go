package config_secret

import (
	"fmt"
	api "github.com/k8ssandra/cass-operator/operator/pkg/apis/cassandra/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	ginkgo_util "github.com/k8ssandra/cass-operator/mage/ginkgo"
	"github.com/k8ssandra/cass-operator/mage/kubectl"
)

var (
	testName   = "Config change rollout with config secret"
	namespace  = "test-config-change-rollout-secret"
	dcName     = "dc1"
	dcYaml     = "../testdata/cluster-with-config-secret.yaml"
 	dcResource = fmt.Sprintf("CassandraDatacenter/%s", dcName)
	dcLabel    = fmt.Sprintf("cassandra.datastax.com/datacenter=%s", dcName)
	secretName = "test-config"
	secretYaml = "../testdata/test-config-secret.yaml"
	updatedSecretYaml = "../testdata/updated-test-config-secret.yaml"
	secretResource = fmt.Sprintf("secret/%s", secretName)
	ns         = ginkgo_util.NewWrapper(testName, namespace)
)

func TestLifecycle(t *testing.T) {
	AfterSuite(func() {
		logPath := fmt.Sprintf("%s/aftersuite", ns.LogDir)
		err := kubectl.DumpAllLogs(logPath).ExecV()
		if err != nil {
			fmt.Printf("\n\tError during dumping logs: %s\n\n", err.Error())
		}
		fmt.Printf("\n\tPost-run logs dumped at: %s\n\n", logPath)
		ns.Terminate()
	})

	RegisterFailHandler(Fail)
	RunSpecs(t, testName)
}

var _ = Describe(testName, func() {
	Context("when in a new cluster", func() {
		Specify("cassandra configuration can be applied with a secret", func() {
			By("creating a namespace")
			err := kubectl.CreateNamespace(namespace).ExecV()
			Expect(err).ToNot(HaveOccurred())

			By("setting up cass-operator resources via helm chart")
			ns.HelmInstall("../../charts/cass-operator-chart")
			ns.WaitForOperatorReady()

			step := "creating config secret"
			k := kubectl.ApplyFiles(secretYaml)
			ns.ExecAndLog(step, k)

			step = "creating datacenter"
			k = kubectl.ApplyFiles(dcYaml)
			ns.ExecAndLog(step, k)
			ns.WaitForDatacenterReadyWithTimeouts(dcName, 420, 30)

			step = "update config secret"
			k = kubectl.ApplyFiles(updatedSecretYaml)
			ns.ExecAndLog(step, k)

			ns.WaitForDatacenterOperatorProgress(dcName, "Updating", 30)
			ns.WaitForDatacenterConditionWithTimeout(dcName, string(api.DatacenterReady), string(corev1.ConditionTrue), 450)

			step = "checking cassandra.yaml"
			k = kubectl.ExecOnPod("cluster1-dc1-r1-sts-0", "-c", "cassandra", "--", "cat", "/etc/cassandra/cassandra.yaml")
			ns.WaitForOutputContainsAndLog(step, k, "read_request_timeout_in_ms: 10000", 60)

			step = "stop using config secret"
			json := `[{"op": "remove", "path": "/spec/configSecret"}, {"op": "add", "path": "/spec/config", "value": {"cassandra-yaml": {"read_request_timeout_in_ms": 25000}, "jvm-options": {"initial_heap_size": "512M", "max_heap_size": "512M"}}}]`
			k = kubectl.PatchJson(dcResource, json)
			ns.ExecAndLog(step, k)

			ns.WaitForDatacenterOperatorProgress(dcName, "Updating", 120)
			ns.WaitForDatacenterConditionWithTimeout(dcName, string(api.DatacenterReady), string(corev1.ConditionTrue), 450)

			step = "checking cassandra.yaml"
			k = kubectl.ExecOnPod("cluster1-dc1-r1-sts-0", "-c", "cassandra", "--", "cat", "/etc/cassandra/cassandra.yaml")
			ns.WaitForOutputContainsAndLog(step, k, "read_request_timeout_in_ms: 25000", 60)

			step = "use config secret again"
			json = `{"spec": {"config": null, "configSecret": "test-config"}}`
			k = kubectl.PatchMerge(dcResource, json)
			ns.ExecAndLog(step, k)

			ns.WaitForDatacenterOperatorProgress(dcName, "Updating", 120)
			ns.WaitForDatacenterConditionWithTimeout(dcName, string(api.DatacenterReady), string(corev1.ConditionTrue), 450)

			step = "checking cassandra.yaml"
			k = kubectl.ExecOnPod("cluster1-dc1-r1-sts-0", "-c", "cassandra", "--", "cat", "/etc/cassandra/cassandra.yaml")
			ns.WaitForOutputContainsAndLog(step, k, "read_request_timeout_in_ms: 10000", 60)
		})
	})
})
