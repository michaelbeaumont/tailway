package tailnet

import (
	"context"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (ctrlr *GatewayController) handleDeployment(ctx context.Context, gateway metav1.Object, fqdn string) error {
	deployments := appsv1.DeploymentList{}
	if err := ctrlr.List(ctx, &deployments, client.MatchingLabels{fqdnLabel: fqdn}); err != nil {
		return err
	}

	objectName := strings.ReplaceAll(fqdn, ".", "-")

	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objectName,
			Namespace: "tailway-system",
			Labels: map[string]string{
				fqdnLabel: fqdn,
			},
		},
	}
	if len(deployments.Items) > 0 {
		deployment = deployments.Items[0]
	}

	result, err := controllerutil.CreateOrPatch(ctx, ctrlr.Client, &deployment, func() error {
		deployment.Spec = makeDeploymentSpec(fqdn, objectName)

		return nil
	})
	if err != nil {
		return err
	}

	ctrlr.Logger.Info("handled deployment", "op", result, "name", deployment.Name)

	return nil
}

func makeDeploymentSpec(fqdn, objectName string) appsv1.DeploymentSpec {
	var replicas int32 = 1

	parts := strings.SplitN(fqdn, ".", 2)
	machineName := parts[0]

	return appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				fqdnLabel: fqdn,
			},
		},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					fqdnLabel: fqdn,
				},
			},
			Spec: v1.PodSpec{
				ServiceAccountName: "tailway",
				Containers: []v1.Container{
					{
						Name:            "tailway",
						Image:           "michaelbeaumont/tailway:latest",
						ImagePullPolicy: v1.PullNever,
						VolumeMounts: []v1.VolumeMount{{
							MountPath: "/var/run/tailscale",
							Name:      "var-run-tailscale",
						}},
						Args: []string{"machine"},
					},
					{
						Name:  "tailscale",
						Image: "ghcr.io/tailscale/tailscale:latest",
						Env: []v1.EnvVar{
							{Name: "TS_KUBE_SECRET", Value: objectName + "-state"},
							{Name: "TS_USERSPACE", Value: "false"},
							{Name: "TS_HOSTNAME", Value: machineName},
							{Name: "TS_SOCKET", Value: "/var/run/tailscale/tailscaled.sock"},
							{Name: "TS_AUTH_ONCE", Value: "true"},
							{Name: "TS_AUTHKEY", ValueFrom: &v1.EnvVarSource{
								SecretKeyRef: &v1.SecretKeySelector{
									Key: "TS_AUTHKEY",
									LocalObjectReference: v1.LocalObjectReference{
										Name: objectName + "-authkey",
									},
								},
							}},
						},
						VolumeMounts: []v1.VolumeMount{{
							MountPath: "/var/run/tailscale",
							Name:      "var-run-tailscale",
						}},
						SecurityContext: &v1.SecurityContext{
							Capabilities: &v1.Capabilities{
								Add: []v1.Capability{"NET_ADMIN"},
							},
						},
					},
				},
				Volumes: []v1.Volume{{
					Name: "var-run-tailscale",
					VolumeSource: v1.VolumeSource{
						EmptyDir: &v1.EmptyDirVolumeSource{},
					},
				}},
			},
		},
	}
}
