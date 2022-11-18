/*
Copyright 2022 Nutanix, Inc

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

package constants

const (
	ProviderName string = "nutanix"
	ClientName   string = "nutanix-cloud-controller-manager"

	CCMNamespaceKey = "POD_NAMESPACE"

	InstanceType string = "ahv-vm"

	PoweredOffState string = "OFF"
	PoweredOnState  string = "ON"

	CustomPEUUIDLabel   string = "nutanix.com/prism-element-uuid"
	CustomPENameLabel   string = "nutanix.com/prism-element-name"
	CustomHostUUIDLabel string = "nutanix.com/prism-host-uuid"
	CustomHostNameLabel string = "nutanix.com/prism-host-name"

	PrismCentralService string = "PRISM_CENTRAL"
)
