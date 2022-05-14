/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2021 Red Hat, Inc.
 *
 */

package hostdevice

import (
	"fmt"
	"os"
	"strings"

	"kubevirt.io/client-go/log"
	"kubevirt.io/kubevirt/pkg/util"
)

type AddressPool struct {
	addressesByResource map[string][]string
}

// NewAddressPool creates an address pool based on the provided list of resources and
// the environment variables that correspond to it.
func NewAddressPool(resourcePrefix string, resources []string) *AddressPool {
	pool := &AddressPool{
		addressesByResource: make(map[string][]string),
	}
	pool.load(resourcePrefix, resources)
	return pool
}

func (p *AddressPool) load(resourcePrefix string, resources []string) {
	for _, resource := range resources {
		println("RESOURCE: ", resource)
		addressEnvVarName := util.ResourceNameToEnvVar(resourcePrefix, resource)
		addressString, isSet := os.LookupEnv(addressEnvVarName)
		if !isSet {
			log.Log.Warningf("%s not set for resource %s", addressEnvVarName, resource)
			continue
		}

		addressString = strings.TrimSuffix(addressString, ",")
		if addressString != "" {
			p.addressesByResource[resource] = strings.Split(addressString, ",")
		} else {
			p.addressesByResource[resource] = nil
		}
	}
}

// Pop gets the next address available to a particular resource. The
// function makes sure that the allocated address is not allocated to next
// callers, whether they request an address for the same resource or another
// resource (covering cases of addresses that are share by multiple resources).
func (p *AddressPool) Pop(resource string) (string, error) {

	// dont select the first, find us
	println("AddressPool POP received ", resource)

	// the addresspool pop is called by a few different things, not only sriov
	// so we do a bit of a hack to avoid changing the interface for everything
	// by looking at the resource string to see if it might contain a hint
	// of the target network and if so, we store that
	var netname string
	if strings.Contains(resource, "|") {
		n := strings.Split(resource, "|")
		resource, netname = n[0], n[1]
	}
	addresses, exists := p.addressesByResource[resource]
	if !exists {
		return "", fmt.Errorf("resource %s does not exist", resource)
	}

	// dont just select the first pci address, assume they were generated
	// in the order the networks were, so match the network with the pci
	var selectedAddress string
	if len(addresses) > 0 {

		// use netname to find index in list
		envVarName := util.ResourceNameToEnvVar("KUBEVIRT_RESOURCE_CREATE_ORDER", resource)
		netOrderedList, exist := os.LookupEnv(envVarName)
		if exist {
			n := strings.Split(netOrderedList, ",")
			for i := 0; i < len(n); i++ {
				println("Trying n[i]", n[i])
				println("Trying netname ", netname)
				if n[i] == netname {
					// TODO handle out of bounds
					selectedAddress = addresses[i]
					newEnvValue := strings.Join(filterOutOneNetwork(n, netname), ",")
					os.Setenv(envVarName, newEnvValue)
					fmt.Printf("Setting env name=%s\n", envVarName)
					fmt.Printf("Setting env value=%s\n", newEnvValue)
				}
			}
		} else {

			// otherwise, do what we did before if the env var wasnt supplied
			// discover index for this network
			selectedAddress = addresses[0]

		}

		for resourceName, resourceAddresses := range p.addressesByResource {
			newAddresses := filterOutAddress(resourceAddresses, selectedAddress)
			fmt.Println("New Addresses: ", newAddresses)
			p.addressesByResource[resourceName] = newAddresses
		}

		return selectedAddress, nil

	}
	return "", fmt.Errorf("no more addresses to allocate for resource %s", resource)
}

func filterOutAddress(addrs []string, addr string) []string {
	var res []string
	for _, a := range addrs {
		if a != addr {
			res = append(res, a)
		}
	}
	return res
}

func filterOutOneNetwork(networks []string, netName string) []string {
	var res []string
	skipped := false
	for _, a := range networks {
		if (a == netName) && (!skipped) {
			skipped = true
			continue
		} else {
			res = append(res, a)
		}
	}
	return res
}

type BestEffortAddressPool struct {
	pool AddressPooler
}

// NewBestEffortAddressPool creates a pool that wraps a provided pool
// and allows `Pop` calls to always succeed (even when a resource is missing).
func NewBestEffortAddressPool(pool AddressPooler) *BestEffortAddressPool {
	return &BestEffortAddressPool{pool}
}

func (p *BestEffortAddressPool) Pop(resource string) (string, error) {
	address, _ := p.pool.Pop(resource)
	return address, nil
}
