/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package token

import (
	"bytes"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientsetfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	kubeadmapi "k8s.io/kubernetes/cmd/kubeadm/app/apis/kubeadm"
	bootstrapapi "k8s.io/kubernetes/pkg/bootstrap/api"
)

const testConfig = `apiVersion: v1
clusters:
- cluster:
    server: https://10.128.0.6:6443
  name: kubernetes
contexts:
- context:
    cluster: kubernetes
    user: kubernetes-admin
  name: kubernetes-admin@kubernetes
current-context: kubernetes-admin@kubernetes
kind: Config
preferences: {}
users:
- name: kubernetes-admin`

func TestEncodeTokenSecretData(t *testing.T) {
	var tests = []struct {
		token *kubeadmapi.TokenDiscovery
		t     time.Duration
	}{
		{token: &kubeadmapi.TokenDiscovery{ID: "foo", Secret: "bar"}},                 // should use default
		{token: &kubeadmapi.TokenDiscovery{ID: "foo", Secret: "bar"}, t: time.Second}, // should use default
	}
	for _, rt := range tests {
		actual := encodeTokenSecretData(rt.token.ID, rt.token.Secret, rt.t, []string{}, "")
		if !bytes.Equal(actual["token-id"], []byte(rt.token.ID)) {
			t.Errorf(
				"failed EncodeTokenSecretData:\n\texpected: %s\n\t  actual: %s",
				rt.token.ID,
				actual["token-id"],
			)
		}
		if !bytes.Equal(actual["token-secret"], []byte(rt.token.Secret)) {
			t.Errorf(
				"failed EncodeTokenSecretData:\n\texpected: %s\n\t  actual: %s",
				rt.token.Secret,
				actual["token-secret"],
			)
		}
		if rt.t > 0 {
			if actual["expiration"] == nil {
				t.Errorf(
					"failed EncodeTokenSecretData, duration was not added to time",
				)
			}
		}
	}
}

func TestCreateBootstrapConfigMapIfNotExists(t *testing.T) {
	tests := []struct {
		name                     string
		existingConfigmap        *v1.ConfigMap
		expectingConfigmapFields []string
		getErr                   error
		createErr                error
		updateErr                error
		expectErr                bool
	}{
		{
			"successful case should have no error",
			nil,
			[]string{"kubeconfig"},
			apierrors.NewNotFound(v1.Resource("configMap"), "configmap doesn't exist"),
			nil,
			nil,
			false,
		},
		{
			"unexpected error on Get should be returned",
			nil,
			[]string{},
			apierrors.NewUnauthorized("go away!"),
			nil,
			nil,
			true,
		},
		{
			"Error on Update should be returned",
			&v1.ConfigMap{Data: map[string]string{}},
			[]string{},
			nil,
			nil,
			apierrors.NewUnauthorized("go away!"),
			true,
		},
		{
			"Error on Create should be returned",
			nil,
			[]string{},
			apierrors.NewNotFound(v1.Resource("configMap"), "configmap doesn't exist"),
			apierrors.NewUnauthorized("go away!"),
			nil,
			true,
		},
		{
			"update should retain existing configmap data field",
			&v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: bootstrapapi.ConfigMapClusterInfo},
				Data: map[string]string{
					"cluster-id": "some-id",
				},
			},
			[]string{"kubeconfig", "cluster-id"},
			nil,
			nil,
			nil,
			false,
		},
	}

	file, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatalf("could not create tempfile: %v", err)
	}
	defer os.Remove(file.Name())

	file.Write([]byte(testConfig))

	for _, tc := range tests {
		client := clientsetfake.NewSimpleClientset()
		client.PrependReactor("get", "configmaps", func(action core.Action) (bool, runtime.Object, error) {
			return true, tc.existingConfigmap, tc.getErr
		})
		client.PrependReactor("create", "configmaps", func(action core.Action) (bool, runtime.Object, error) {
			return true, nil, tc.createErr
		})
		client.PrependReactor("update", "configmaps", func(action core.Action) (bool, runtime.Object, error) {
			return true, nil, tc.updateErr
		})

		err = UpdateOrCreateBootstrapConfigMapIfNeeded(client, file.Name())
		if tc.expectErr && err == nil {
			t.Errorf("UpdateOrCreateBootstrapConfigMapIfNeeded(%s) wanted error, got nil", tc.name)
		} else if !tc.expectErr && err != nil {
			t.Errorf("UpdateOrCreateBootstrapConfigMapIfNeeded(%s) returned unexpected error: %v", tc.name, err)
		}
		if len(tc.expectingConfigmapFields) > 0 {
			newMap := client.Actions()[1].(core.CreateAction).GetObject()
			for _, field := range tc.expectingConfigmapFields {
				if _, exists := newMap.(*v1.ConfigMap).Data[field]; !exists {
					t.Errorf("UpdateOrCreateBootstrapConfigMapIfNeeded contains missing data field(%s)", field)
				}

			}
		}
	}
}
