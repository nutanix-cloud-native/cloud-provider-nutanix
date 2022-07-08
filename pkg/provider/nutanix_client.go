package provider

import (
	"context"
	"encoding/json"
	"fmt"

	prismClient "github.com/nutanix-cloud-native/prism-go-client/pkg/nutanix"
	prismClientV3 "github.com/nutanix-cloud-native/prism-go-client/pkg/nutanix/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type nutanixClientHelper struct {
	kClient clientset.Interface
}

func (nc *nutanixClientHelper) create(config Config) (*prismClientV3.Client, error) {
	creds, err := nc.getPrismCentralCredentialFromConfig(config)
	if err != nil {
		return nil, err
	}
	nutanixClient, err := prismClientV3.NewV3Client(*creds)
	if err != nil {
		return nil, err
	}

	_, err = nutanixClient.V3.GetCurrentLoggedInUser(context.Background())
	if err != nil {
		return nil, err
	}

	return nutanixClient, nil
}

func (nc *nutanixClientHelper) getPrismCentralCredentialFromConfig(config Config) (*prismClient.Credentials, error) {
	prismCentralInfo := config.PrismCentral
	if prismCentralInfo.Address == "" {
		return nil, fmt.Errorf("cannot get credentials if Prism Address is not set")
	}
	if prismCentralInfo.Port == 0 {
		return nil, fmt.Errorf("cannot get credentials if Prism Port is not set")
	}
	portStr := fmt.Sprint(prismCentralInfo.Port)
	credentials := &prismClient.Credentials{
		Insecure: prismCentralInfo.Insecure,
		Port:     portStr,
		Endpoint: prismCentralInfo.Address,
		URL:      fmt.Sprintf("%s:%s", prismCentralInfo.Address, portStr),
	}
	credentialRef := config.PrismCentral.CredentialRef
	if credentialRef == nil {
		return nil, fmt.Errorf("credentialRef must be set when creating Prism Central credentials")
	}
	credentials, err := nc.withCredentialFromRef(credentialRef, credentials)
	if err != nil {
		return nil, err
	}
	return credentials, nil
}

func (nc *nutanixClientHelper) withCredentialFromRef(credentialRef *NutanixCredentialReference, credential *prismClient.Credentials) (*prismClient.Credentials, error) {
	ctx := context.Background()
	if credentialRef.Name == "" {
		return nil, fmt.Errorf("name must be set on credentialRef")
	}
	credentialRefnamespace := defaultCCMSecretNamespace
	if credentialRef.Namespace != "" {
		credentialRefnamespace = credentialRef.Namespace
	}
	secret, err := nc.kClient.CoreV1().Secrets(credentialRefnamespace).Get(ctx, credentialRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	newCred := *credential

	credsData, ok := secret.Data["credentials"]
	if !ok {
		return nil, fmt.Errorf("no credentials data found in secret %s in namespace %s", credentialRef.Name, credentialRefnamespace)
	}
	creds := &NutanixCredentials{}
	err = json.Unmarshal(credsData, &creds.Credentials)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal the credentials data. %w", err)
	}
	for _, cred := range creds.Credentials {
		switch cred.Type {
		case BasicAuthCredentialType:
			basicAuthCreds := BasicAuthCredential{}
			if err := json.Unmarshal(cred.Data.Raw, &basicAuthCreds); err != nil {
				return nil, fmt.Errorf("failed to unmarshal the basic-auth data. %w", err)
			}
			if basicAuthCreds.PrismCentral.Username == "" || basicAuthCreds.PrismCentral.Password == "" {
				return nil, fmt.Errorf("the PrismCentral credentials data is not set for secret %s in namespace %s.", credentialRef.Name, credentialRefnamespace)
			}

			newCred.Username = basicAuthCreds.PrismCentral.Username
			newCred.Password = basicAuthCreds.PrismCentral.Password
			klog.V(1).Info("successfully set the PrismCentral credentials in order to create the Prism Central client.")
			return &newCred, nil

		default:
			return nil, fmt.Errorf("unsupported credentials type in secret %s in namespace %s: %v", credentialRef.Name, credentialRefnamespace, cred.Type)
		}
	}
	return nil, fmt.Errorf("the PrismCentral credentials data is not available in secret %s in namespace %s.", credentialRef.Name, credentialRefnamespace)
}
