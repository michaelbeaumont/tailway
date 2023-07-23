package tailnet

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1beta1"
	"tailscale.com/client/tailscale"
)

func (ctrlr *GatewayController) handleSecret(ctx context.Context, gateway metav1.Object, class *gatewayapi.GatewayClass, fqdn string) error {
	objectName := strings.ReplaceAll(fqdn, ".", "-")

	secrets := v1.SecretList{}
	if err := ctrlr.List(ctx, &secrets, client.MatchingLabels{
		fqdnLabel: fqdn,
	}); err != nil {
		return err
	}

	if len(secrets.Items) > 0 {
		return nil
	}

	tagsAnnotation, ok := class.Annotations[tagsAnnotation]
	tags := strings.Split(tagsAnnotation, ",")

	caps := tailscale.KeyCapabilities{
		Devices: tailscale.KeyDeviceCapabilities{
			Create: tailscale.KeyDeviceCreateCapabilities{
				Reusable:      false,
				Preauthorized: true,
				Tags:          tags,
			},
		},
	}

	ctrlr.tsClients.Lock()
	ts, ok := ctrlr.tsClients.clients[class.Name]
	ctrlr.tsClients.Unlock()
	if !ok {
		return errors.New("couldn't find TS client in map")
	}

	key, _, err := ts.CreateKey(ctx, caps)
	if err != nil {
		return errors.Wrap(err, "couldn't create authkey")
	}

	authkeySecret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      objectName + "-authkey",
			Namespace: "tailway-system",
			Labels: map[string]string{
				fqdnLabel: fqdn,
			},
		},
		StringData: map[string]string{
			"TS_AUTHKEY": key,
		},
	}

	if err := ctrlr.Create(ctx, &authkeySecret); err != nil {
		return err
	}

	return nil
}
