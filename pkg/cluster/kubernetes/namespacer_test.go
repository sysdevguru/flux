package kubernetes

import (
	"io/ioutil"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	kresource "github.com/fluxcd/flux/pkg/cluster/kubernetes/resource"
)

type mockScoper struct {
	namespacedGroupKinds []schema.GroupKind
}

func (m *mockScoper) IsNamespaced(gk schema.GroupKind) bool {
	for _, namespacedGK := range m.namespacedGroupKinds {
		if gk == namespacedGK {
			return true
		}
	}
	return false
}

func newNamespacer(defaultNamespace string, scoper scoper) (*namespacerViaScoper, error) {
	fallbackNamespace, err := getFallbackNamespace(defaultNamespace)
	if err != nil {
		return nil, err
	}
	return &namespacerViaScoper{scoper: scoper, fallbackNamespace: fallbackNamespace}, nil
}

func TestNamespaceDefaulting(t *testing.T) {
	testKubeconfig := `apiVersion: v1
clusters: []
contexts:
- context:
    cluster: cluster
    namespace: namespace
    user: user
  name: context
current-context: context
kind: Config
preferences: {}
users: []
`
	err := ioutil.WriteFile("testkubeconfig", []byte(testKubeconfig), 0600)
	if err != nil {
		t.Fatal("cannot create test kubeconfig file")
	}
	defer os.Remove("testkubeconfig")

	os.Setenv("KUBECONFIG", "testkubeconfig")
	defer os.Unsetenv("KUBECONFIG")

	ns, err := getKubeconfigDefaultNamespace()
	if err != nil {
		t.Fatal("cannot get default namespace")
	}
	if ns != "namespace" {
		t.Fatal("unexpected default namespace", ns)
	}

	const defs = `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hasNamespace
  namespace: foo-ns
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: noNamespace
---
apiVersion: v1
kind: Namespace
metadata:
  name: notNamespaced
  namespace: spurious
`

	manifests, err := kresource.ParseMultidoc([]byte(defs), "<string>")
	if err != nil {
		t.Fatal(err)
	}

	scoper := &mockScoper{[]schema.GroupKind{{"apps", "Deployment"}}}
	defaultNser, err := newNamespacer("", scoper)
	if err != nil {
		t.Fatal(err)
	}
	assertEffectiveNamespace := func(nser namespacerViaScoper, id, expected string) {
		res, ok := manifests[id]
		if !ok {
			t.Errorf("manifest for %q not found", id)
			return
		}
		got, err := nser.EffectiveNamespace(res, nil)
		if err != nil {
			t.Errorf("error getting effective namespace for %q: %s", id, err.Error())
			return
		}
		if got != expected {
			t.Errorf("expected effective namespace of %q, got %q", expected, got)
		}
	}

	assertEffectiveNamespace(*defaultNser, "foo-ns:deployment/hasNamespace", "foo-ns")
	assertEffectiveNamespace(*defaultNser, "<cluster>:deployment/noNamespace", "namespace")
	assertEffectiveNamespace(*defaultNser, "spurious:namespace/notNamespaced", "")

	overrideNser, err := newNamespacer("foo-override", scoper)
	if err != nil {
		t.Fatal(err)
	}

	assertEffectiveNamespace(*overrideNser, "foo-ns:deployment/hasNamespace", "foo-ns")
	assertEffectiveNamespace(*overrideNser, "<cluster>:deployment/noNamespace", "foo-override")
	assertEffectiveNamespace(*overrideNser, "spurious:namespace/notNamespaced", "")

}
