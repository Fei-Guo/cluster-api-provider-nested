/*
Copyright 2021 The Kubernetes Authors.

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

package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"sigs.k8s.io/controller-runtime/pkg/client"

	tenancyv1alpha1 "sigs.k8s.io/cluster-api-provider-nested/virtualcluster/pkg/apis/tenancy/v1alpha1"
	"sigs.k8s.io/cluster-api-provider-nested/virtualcluster/pkg/controller/secret"
	"sigs.k8s.io/cluster-api-provider-nested/virtualcluster/pkg/syncer/constants"
)

func getClusterObjectKey(instance *tenancyv1alpha1.VirtualCluster, name string) client.ObjectKey {
	return client.ObjectKey{Name: name, Namespace: instance.Status.ClusterNamespace}
}

var _ = Describe("VirtualCluster Controller", func() {

	Context("Reconcile VirtualCluster Cluster", func() {
		var cvInstance *tenancyv1alpha1.ClusterVersion
		var instance *tenancyv1alpha1.VirtualCluster

		It("Should create resources successfully", func() {
			ctx := context.TODO()
			Expect(cli).ShouldNot(BeNil())

			cvInstance = createClusterVersion()
			Expect(cli.Create(ctx, cvInstance)).Should(Succeed())

			By("Fetching ClusterVersion")
			Eventually(func() bool {
				cvObjectKey := client.ObjectKeyFromObject(cvInstance)
				err := cli.Get(ctx, cvObjectKey, cvInstance)
				return !apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			instance = &tenancyv1alpha1.VirtualCluster{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "virtualcluster-sample",
					Namespace:    "default",
				},
				Spec: tenancyv1alpha1.VirtualClusterSpec{
					ClusterVersionName: cvInstance.GetName(),
				},
			}
			Expect(cli.Create(ctx, instance)).Should(Succeed())

			objectKey := client.ObjectKeyFromObject(instance)

			By("Adding Finalizer")
			Eventually(func() bool {
				err := cli.Get(ctx, objectKey, instance)
				return err == nil && len(instance.GetFinalizers()) == 1
			}, timeout, interval).Should(BeTrue())

			nsObjectKey := client.ObjectKey{Name: instance.Status.ClusterNamespace}
			Expect(nsObjectKey.Name).ToNot(BeEmpty())

			By("Creating Root Namespace")
			Eventually(func() bool {
				ns := &corev1.Namespace{}
				err := cli.Get(ctx, nsObjectKey, ns)
				return !apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			By("Creating APIServer Service")
			Eventually(func() bool {
				svcObjectKey := getClusterObjectKey(instance, "apiserver-svc")
				svc := &corev1.Service{}
				err := cli.Get(ctx, svcObjectKey, svc)
				return !apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			expectedPKI := []string{
				secret.RootCASecretName,
				secret.APIServerCASecretName,
				secret.ETCDCASecretName,
				secret.ControllerManagerSecretName,
				secret.AdminSecretName,
				secret.ServiceAccountSecretName,
			}

			By("Creating Control Plane PKI")
			for _, pki := range expectedPKI {
				Eventually(func() bool {
					sctObjectKey := getClusterObjectKey(instance, pki)
					sct := &corev1.Secret{}
					err := cli.Get(ctx, sctObjectKey, sct)
					return !apierrors.IsNotFound(err)
				}, timeout, interval).Should(BeTrue())
			}

			By("Creating Control Plane etcd Service")
			Eventually(func() bool {
				svcObjectKey := getClusterObjectKey(instance, "etcd")
				svc := &corev1.Service{}
				err := cli.Get(ctx, svcObjectKey, svc)
				return !apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			etcdSts := &appsv1.StatefulSet{}

			By("Creating Control Plane etcd StatefulSet")
			Eventually(func() bool {
				stsObjectKey := getClusterObjectKey(instance, "etcd")
				err := cli.Get(ctx, stsObjectKey, etcdSts)
				return !apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			etcdSts.Status.Replicas = 1
			etcdSts.Status.ReadyReplicas = 1
			By("Faking etcd STS Status Updates")
			err := cli.Status().Update(ctx, etcdSts)
			Expect(err).To(BeNil())

			apiserverSts := &appsv1.StatefulSet{}
			By("Creating Control Plane apiserver StatefulSet")
			Eventually(func() bool {
				stsObjectKey := getClusterObjectKey(instance, "apiserver")
				err := cli.Get(ctx, stsObjectKey, apiserverSts)
				return !apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			apiserverSts.Status.Replicas = 1
			apiserverSts.Status.ReadyReplicas = 1
			By("Faking apiserver STS Status Updates")
			err = cli.Status().Update(ctx, apiserverSts)
			Expect(err).To(BeNil())

			cmSts := &appsv1.StatefulSet{}
			By("Creating Control Plane controller-manager StatefulSet")
			Eventually(func() bool {
				stsObjectKey := getClusterObjectKey(instance, "controller-manager")
				err := cli.Get(ctx, stsObjectKey, cmSts)
				return !apierrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			cmSts.Status.Replicas = 1
			cmSts.Status.ReadyReplicas = 1
			By("Faking controller-manager STS Status Updates")
			err = cli.Status().Update(ctx, cmSts)
			Expect(err).To(BeNil())
		})
		It("Should upgrade resources successfully", func() {
			ctx := context.TODO()
			Expect(cli).ShouldNot(BeNil())

			objectKey := client.ObjectKeyFromObject(instance)

			By("Checking cluster phase")
			Eventually(func() bool {
				err := cli.Get(ctx, objectKey, instance)
				return err == nil && instance.Status.Phase == tenancyv1alpha1.ClusterRunning
			}, time.Minute*5, interval).Should(BeTrue())

			By("Updating ClusterVersion")
			cvInstance.Spec.APIServer.StatefulSet.Spec.Template.Spec.Containers[0].Args = append([]string{"-v=7"}, cvInstance.Spec.APIServer.StatefulSet.Spec.Template.Spec.Containers[0].Args...)
			if cvInstance.Spec.APIServer.Service.Labels == nil {
				cvInstance.Spec.APIServer.Service.Labels = map[string]string{}
			}
			cvInstance.Spec.APIServer.Service.Labels["test-label"] = "test"
			cvInstance.Spec.ControllerManager.StatefulSet.Spec.Template.Spec.Containers[0].Args = append([]string{"-v=7"}, cvInstance.Spec.ControllerManager.StatefulSet.Spec.Template.Spec.Containers[0].Args...)
			cvInstance.ObjectMeta.ManagedFields = nil
			forceTrue := true
			Expect(cli.Patch(ctx, cvInstance, client.Apply, &client.PatchOptions{Force: &forceTrue, FieldManager: "test"})).Should(Succeed())

			By("Enable upgrade for cluster")
			instance.Labels[constants.LabelVCReadyForUpgrade] = "true"
			instance.ObjectMeta.ManagedFields = nil
			Expect(cli.Patch(ctx, instance, client.Apply, &client.PatchOptions{Force: &forceTrue, FieldManager: "test"})).Should(Succeed())

			By("APIServer Service upgraded")
			Eventually(func() bool {
				svcObjectKey := getClusterObjectKey(instance, "apiserver-svc")
				svc := &corev1.Service{}
				err := cli.Get(ctx, svcObjectKey, svc)
				return !apierrors.IsNotFound(err) && svc.Labels["test-label"] == "test"
			}, time.Minute*2, interval).Should(BeTrue())

			apiserverSts := &appsv1.StatefulSet{}
			By("Control Plane apiserver StatefulSet upgraded")
			Eventually(func() bool {
				stsObjectKey := getClusterObjectKey(instance, "apiserver")
				err := cli.Get(ctx, stsObjectKey, apiserverSts)
				return !apierrors.IsNotFound(err) && apiserverSts.Spec.Template.Spec.Containers[0].Args[0] == "-v=7"
			}, time.Minute*2, interval).Should(BeTrue())

			cmSts := &appsv1.StatefulSet{}
			By("Control Plane controller-manager StatefulSet upgraded")
			Eventually(func() bool {
				stsObjectKey := getClusterObjectKey(instance, "controller-manager")
				err := cli.Get(ctx, stsObjectKey, cmSts)
				return !apierrors.IsNotFound(err) && cmSts.Spec.Template.Spec.Containers[0].Args[0] == "-v=7"
			}, time.Minute*2, interval).Should(BeTrue())
		})
		It("Should delete cluster", func() {
			By("Deleting VirtualCluster")
			Expect(cli.Delete(context.TODO(), instance)).To(BeNil())
		})
	})
})
