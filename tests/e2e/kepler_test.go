package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/sustainable.computing.io/kepler-operator/pkg/api/v1alpha1"
	"github.com/sustainable.computing.io/kepler-operator/pkg/components"
	"github.com/sustainable.computing.io/kepler-operator/pkg/components/exporter"
	"github.com/sustainable.computing.io/kepler-operator/pkg/utils/k8s"
	"github.com/sustainable.computing.io/kepler-operator/pkg/utils/test"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestKepler_Deletion(t *testing.T) {
	f := test.NewFramework(t)

	// pre-condition: ensure kepler exists
	f.CreateKepler("kepler")
	f.WaitUntilKeplerCondition("kepler", v1alpha1.Available, v1alpha1.ConditionTrue)

	//
	ds := appsv1.DaemonSet{}
	f.AssertResourceExists(
		exporter.DaemonSetName,
		components.Namespace,
		&ds,
		test.Timeout(10*time.Second),
	)

	f.DeleteKepler("kepler")

	ns := components.NewKeplerNamespace()
	f.AssertNoResourceExists(ns.Name, "", ns)
	f.AssertNoResourceExists(exporter.DaemonSetName, components.Namespace, &ds)
}

func TestKepler_Reconciliation(t *testing.T) {
	f := test.NewFramework(t)

	// pre-condition
	f.AssertNoResourceExists("kepler", "", &v1alpha1.Kepler{}, test.NoWait())

	// when
	f.CreateKepler("kepler")

	// then
	f.AssertResourceExists(components.Namespace, "", &corev1.Namespace{})
	ds := appsv1.DaemonSet{}
	f.AssertResourceExists(exporter.DaemonSetName, components.Namespace, &ds)

	kepler := f.WaitUntilKeplerCondition("kepler", v1alpha1.Reconciled, v1alpha1.ConditionTrue)
	// ensure the default toleration is set
	assert.Equal(t, []corev1.Toleration{{Operator: "Exists"}}, kepler.Spec.Exporter.Deployment.Tolerations)

	reconciled, err := k8s.FindCondition(kepler.Status.Conditions, v1alpha1.Reconciled)
	assert.NoError(t, err, "unable to get reconciled condition")
	assert.Equal(t, reconciled.ObservedGeneration, kepler.Generation)
	assert.Equal(t, reconciled.Status, v1alpha1.ConditionTrue)

	kepler = f.WaitUntilKeplerCondition("kepler", v1alpha1.Available, v1alpha1.ConditionTrue)
	available, err := k8s.FindCondition(kepler.Status.Conditions, v1alpha1.Available)
	assert.NoError(t, err, "unable to get available condition")
	assert.Equal(t, available.ObservedGeneration, kepler.Generation)
	assert.Equal(t, available.Status, v1alpha1.ConditionTrue)

}

func TestBadKepler_Reconciliation(t *testing.T) {
	f := test.NewFramework(t)
	// Ensure Kepler is not deployed (by any chance)
	f.AssertNoResourceExists("kepler", "", &v1alpha1.Kepler{}, test.Timeout(10*time.Second))
	f.AssertNoResourceExists("invalid-name", "", &v1alpha1.Kepler{}, test.NoWait())
	f.CreateKepler("invalid-name")

	ds := appsv1.DaemonSet{}
	f.AssertNoResourceExists(exporter.DaemonSetName, components.Namespace, &ds)
}

func TestNodeSelector(t *testing.T) {
	f := test.NewFramework(t)
	// Ensure Kepler is not deployed (by any chance)
	f.AssertNoResourceExists("kepler", "", &v1alpha1.Kepler{}, test.Timeout(10*time.Second))

	nodes := f.GetSchedulableNodes()
	assert.NotZero(t, len(nodes), "got zero nodes")

	node := nodes[0]
	var labels k8s.StringMap = map[string]string{"e2e-test": "true"}
	err := f.AddResourceLabels("node", node.Name, labels)
	assert.NoError(t, err, "could not label node")

	f.CreateKepler("kepler", func(k *v1alpha1.Kepler) {
		k.Spec.Exporter.Deployment.NodeSelector = labels
	})

	f.AssertResourceExists(components.Namespace, "", &corev1.Namespace{})
	ds := appsv1.DaemonSet{}
	f.AssertResourceExists(exporter.DaemonSetName, components.Namespace, &ds)

	kepler := f.WaitUntilKeplerCondition("kepler", v1alpha1.Available, v1alpha1.ConditionTrue)
	assert.EqualValues(t, 1, kepler.Status.NumberAvailable)

	f.DeleteKepler("kepler")

	ns := components.NewKeplerNamespace()
	f.AssertNoResourceExists(ns.Name, "", ns)
	f.AssertNoResourceExists(exporter.DaemonSetName, components.Namespace, &ds)
}

func TestNodeSelectorUnAvailableLabel(t *testing.T) {
	f := test.NewFramework(t)
	// Ensure Kepler is not deployed (by any chance)
	f.AssertNoResourceExists("kepler", "", &v1alpha1.Kepler{}, test.Timeout(10*time.Second))

	nodes := f.GetSchedulableNodes()
	assert.NotZero(t, len(nodes), "got zero nodes")

	var unavailableLabel k8s.StringMap = map[string]string{"e2e-test": "true"}

	f.CreateKepler("kepler", func(k *v1alpha1.Kepler) {
		k.Spec.Exporter.Deployment.NodeSelector = unavailableLabel
	})

	f.AssertResourceExists(components.Namespace, "", &corev1.Namespace{})
	ds := appsv1.DaemonSet{}
	f.AssertResourceExists(exporter.DaemonSetName, components.Namespace, &ds)

	kepler := f.WaitUntilKeplerCondition("kepler", v1alpha1.Available, v1alpha1.ConditionFalse)
	assert.EqualValues(t, 0, kepler.Status.NumberAvailable)

	f.DeleteKepler("kepler")

	ns := components.NewKeplerNamespace()
	f.AssertNoResourceExists(ns.Name, "", ns)
	f.AssertNoResourceExists(exporter.DaemonSetName, components.Namespace, &ds)
}

func TestTaint_WithToleration(t *testing.T) {

	f := test.NewFramework(t)
	// Ensure Kepler is not deployed (by any chance)
	f.AssertNoResourceExists("kepler", "", &v1alpha1.Kepler{}, test.Timeout(10*time.Second))

	var err error
	// choose one node
	nodes := f.GetSchedulableNodes()
	node := nodes[0]

	e2eTestTaint := corev1.Taint{
		Key:    "key1",
		Value:  "value1",
		Effect: corev1.TaintEffectNoSchedule,
	}

	err = f.TaintNode(node.Name, e2eTestTaint.ToString())
	assert.NoError(t, err, "failed to taint node %s", node)

	f.CreateKepler("kepler", func(k *v1alpha1.Kepler) {
		k.Spec.Exporter.Deployment.Tolerations = f.TolerateTaints(append(node.Spec.Taints, e2eTestTaint))
	})

	f.AssertResourceExists(components.Namespace, "", &corev1.Namespace{})
	ds := appsv1.DaemonSet{}
	f.AssertResourceExists(exporter.DaemonSetName, components.Namespace, &ds)

	kepler := f.WaitUntilKeplerCondition("kepler", v1alpha1.Available, v1alpha1.ConditionTrue)
	assert.EqualValues(t, len(nodes), kepler.Status.NumberAvailable)

	f.DeleteKepler("kepler")

	ns := components.NewKeplerNamespace()
	f.AssertNoResourceExists(ns.Name, "", ns)
	f.AssertNoResourceExists(exporter.DaemonSetName, components.Namespace, &ds)

}
func TestBadTaint_WithToleration(t *testing.T) {

	f := test.NewFramework(t)
	// Ensure Kepler is not deployed (by any chance)
	f.AssertNoResourceExists("kepler", "", &v1alpha1.Kepler{}, test.Timeout(10*time.Second))

	// choose one node
	nodes := f.GetSchedulableNodes()
	node := nodes[0]
	e2eTestTaint := corev1.Taint{
		Key:    "key1",
		Value:  "value1",
		Effect: corev1.TaintEffectNoSchedule,
	}
	badTestTaint := corev1.Taint{
		Key:    "key2",
		Value:  "value2",
		Effect: corev1.TaintEffectNoSchedule,
	}

	err := f.TaintNode(node.Name, e2eTestTaint.ToString())
	assert.NoError(t, err, "failed to taint node %s", node)

	f.CreateKepler("kepler", func(k *v1alpha1.Kepler) {
		k.Spec.Exporter.Deployment.Tolerations = f.TolerateTaints(append(node.Spec.Taints, badTestTaint))
	})

	f.AssertResourceExists(components.Namespace, "", &corev1.Namespace{})
	ds := appsv1.DaemonSet{}
	f.AssertResourceExists(exporter.DaemonSetName, components.Namespace, &ds)

	kepler := f.WaitUntilKeplerCondition("kepler", v1alpha1.Available, v1alpha1.ConditionTrue)
	assert.EqualValues(t, len(nodes)-1, kepler.Status.NumberAvailable)

	f.DeleteKepler("kepler")

	ns := components.NewKeplerNamespace()
	f.AssertNoResourceExists(ns.Name, "", ns)
	f.AssertNoResourceExists(exporter.DaemonSetName, components.Namespace, &ds)

}
