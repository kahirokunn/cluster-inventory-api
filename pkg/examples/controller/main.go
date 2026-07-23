package main

import (
	"flag"
	"log"

	"sigs.k8s.io/cluster-inventory-api/apis/v1alpha2"
	"sigs.k8s.io/cluster-inventory-api/pkg/access"
)

func main() {
	providerFile := access.SetupProviderFileFlag()
	flag.Parse()

	accessCfg, err := access.NewFromFile(*providerFile)
	if err != nil {
		log.Fatalf("Got error reading access providers: %v", err)
	}

	// normally we would get this clusterprofile from the local cluster (maybe a watch?)
	// and we would maintain the restconfigs for clusters we're interested in.
	exampleClusterProfile := v1alpha2.ClusterProfile{
		Spec: v1alpha2.ClusterProfileSpec{
			DisplayName: "My Cluster",
		},
		Status: v1alpha2.ClusterProfileStatus{
			AccessProviders: []v1alpha2.AccessProvider{
				{
					Name: "gkeFleet",
					Cluster: v1alpha2.Cluster{
						Server: "https://myserver.tld:443",
					},
				},
			},
		},
	}

	restConfigForMyCluster, err := accessCfg.BuildConfigFromCP(&exampleClusterProfile)
	if err != nil {
		log.Fatalf("Got error generating restConfig: %v", err)
	}
	log.Printf("Got rest.Config: %v", restConfigForMyCluster)
	// I can then use this rest.Config to build a k8s client.
}
