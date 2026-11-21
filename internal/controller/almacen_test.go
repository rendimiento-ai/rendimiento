package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// A volume that exists keeps the storage class it was made with when
// STORAGE_CLASS changes (a claim's class cannot change); a new one takes
// the new class.
func TestVolumeKeepsItsStorageClass(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	viejo := "longhorn"
	live := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: "almacen-fijo", Name: "web-data"},
		Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &viejo}}
	r := &AppReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(live).Build(), Scheme: scheme}
	nuevo := "local-path"
	want := func(ns, name string) *corev1.PersistentVolumeClaim {
		c := nuevo
		return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &c}}
	}
	existe := want("almacen-fijo", "web-data")
	r.keepStorageClass(context.Background(), existe)
	if *existe.Spec.StorageClassName != "longhorn" {
		t.Fatalf("the existing volume would move to %s", *existe.Spec.StorageClassName)
	}
	otro := want("almacen-fijo", "api-data")
	r.keepStorageClass(context.Background(), otro)
	if *otro.Spec.StorageClassName != "local-path" {
		t.Fatalf("a new volume has %s", *otro.Spec.StorageClassName)
	}
}
