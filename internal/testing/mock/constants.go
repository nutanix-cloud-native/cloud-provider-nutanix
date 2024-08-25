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

package mock

const (
	MockIP           = "1.1.1.1"
	MockCluster      = "mock-cluster"
	MockPrismCentral = "mock-pc"
	MockRegion       = "mock-region"
	MockZone         = "mock-zone"

	MockDefaultRegion = "region"
	MockDefaultZone   = "zone"

	MockVMNamePoweredOn                  = "mock-vm-poweredon"
	MockVMNamePoweredOff                 = "mock-vm-poweredoff"
	MockVMNameCategories                 = "mock-vm-categories"
	MockVMNameNoAddresses                = "mock-vm-no-addresses"
	MockVMNameFilteredNodeAddresses      = "mock-vm-filtered-node-addresses"
	MockVMNamePoweredOnClusterCategories = "mock-vm-poweredon-cluster-categories"

	MockNodeNameVMNotExisting = "mock-node-no-vm-exists"
	MockNodeNameNoSystemUUID  = "mock-node-no-system-uuid"

	entityNotFoundError = "ENTITY_NOT_FOUND"

	mockHost              = "mock-host"
	mockAddress           = "mock-address"
	mockCredentialRef     = "mock-cred"
	mockNamespace         = "mock-namespace"
	mockPort              = 9440
	mockInsecure          = false
	mockClusterCategories = "mock-cluster-categories"
)
