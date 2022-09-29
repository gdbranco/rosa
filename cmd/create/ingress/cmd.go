/*
Copyright (c) 2020 Red Hat, Inc.

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

package ingress

import (
	"fmt"
	"os"
	"strings"

	cmv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"
	"github.com/spf13/cobra"

	"github.com/openshift/rosa/pkg/interactive"
	"github.com/openshift/rosa/pkg/ocm"
	"github.com/openshift/rosa/pkg/rosa"
)

var args struct {
	private    bool
	labelMatch string
	nlb        bool
}

var Cmd = &cobra.Command{
	Use:     "ingress",
	Aliases: []string{"route", "routes", "ingresses"},
	Short:   "Add Ingress to cluster",
	Long:    "Add an Ingress endpoint to determine API access to the cluster.",
	Example: `  # Add an internal ingress to a cluster named "mycluster"
  rosa create ingress --private --cluster=mycluster

  # Add a public ingress to a cluster
  rosa create ingress --cluster=mycluster

  # Add an ingress with route selector label match
  rosa create ingress -c mycluster --label-match="foo=bar,bar=baz"`,
	Run: run,
}

func init() {
	flags := Cmd.Flags()

	ocm.AddClusterFlag(Cmd)

	flags.BoolVar(
		&args.private,
		"private",
		false,
		"Restrict application route to direct, private connectivity.",
	)

	flags.StringVar(
		&args.labelMatch,
		"label-match",
		"",
		"Label match for ingress. Format should be a comma-separated list of 'key=value'. "+
			"If no label is specified, all routes will be exposed on both routers.",
	)

	flags.BoolVar(
		&args.nlb,
		"nlb",
		false,
		"Chooses type of load balancer to be NLB.",
	)

	interactive.AddFlag(flags)
}

func run(cmd *cobra.Command, _ []string) {
	r := rosa.NewRuntime().WithAWS().WithOCM()
	defer r.Cleanup()

	clusterKey := r.GetClusterKey()
	var err error
	labelMatch := args.labelMatch
	if interactive.Enabled() {
		labelMatch, err = interactive.GetString(interactive.Input{
			Question: "Label match for ingress",
			Help:     cmd.Flags().Lookup("label-match").Usage,
			Default:  labelMatch,
			Validators: []interactive.Validator{
				labelValidator,
			},
		})
		if err != nil {
			r.Reporter.Errorf("Expected a valid comma-separated list of attributes: %s", err)
			os.Exit(1)
		}
	}
	routeSelectors, err := getRouteSelector(labelMatch)
	if err != nil {
		r.Reporter.Errorf("%s", err)
		os.Exit(1)
	}

	cluster := r.FetchCluster()
	if cluster.AWS().PrivateLink() {
		r.Reporter.Errorf("Cluster '%s' is PrivateLink and does not support creating new ingresses", clusterKey)
		os.Exit(1)
	}

	if cluster.State() != cmv1.ClusterStateReady {
		r.Reporter.Errorf("Cluster '%s' is not yet ready", clusterKey)
		os.Exit(1)
	}

	ingressBuilder := cmv1.NewIngress()

	private := args.private
	if interactive.Enabled() {
		private, err = interactive.GetBool(interactive.Input{
			Question: "Private ingress",
			Help:     cmd.Flags().Lookup("private").Usage,
			Default:  args.private,
		})
		if err != nil {
			r.Reporter.Errorf("Expected a valid private value: %s", err)
			os.Exit(1)
		}
	}

	nlb := args.nlb
	if interactive.Enabled() {
		nlb, err = interactive.GetBool(interactive.Input{
			Question: "Network Load Balancer",
			Help:     cmd.Flags().Lookup("nlb").Usage,
			Default:  args.nlb,
		})
		if err != nil {
			r.Reporter.Errorf("Expected a valid nlb value: %s", err)
			os.Exit(1)
		}
	}

	if private {
		ingressBuilder = ingressBuilder.Listening(cmv1.ListeningMethodInternal)
	} else {
		ingressBuilder = ingressBuilder.Listening(cmv1.ListeningMethodExternal)
	}

	if nlb {
		ingressBuilder = ingressBuilder.LoadBalancerType(cmv1.NetworkLoadBalancer)
	} else {
		ingressBuilder = ingressBuilder.LoadBalancerType(cmv1.ClassicLoadBalancer)
	}

	if len(routeSelectors) > 0 {
		ingressBuilder = ingressBuilder.RouteSelectors(routeSelectors)
	}
	ingress, err := ingressBuilder.Build()
	if err != nil {
		r.Reporter.Errorf("Failed to create ingress for cluster '%s': %v", clusterKey, err)
		os.Exit(1)
	}

	_, err = r.OCMClient.CreateIngress(cluster.ID(), ingress)
	if err != nil {
		r.Reporter.Errorf("Failed to add ingress to cluster '%s': %s", clusterKey, err)
		os.Exit(1)
	}

	r.Reporter.Infof("Ingress has been created on cluster '%s'.", clusterKey)
	r.Reporter.Infof("To view all ingresses, run 'rosa list ingresses -c %s'", clusterKey)
}

func labelValidator(val interface{}) error {
	if labelMatch, ok := val.(string); ok {
		_, err := getRouteSelector(labelMatch)
		if err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("can only validate strings, got %v", val)
}

func getRouteSelector(labelMatch string) (map[string]string, error) {
	routeSelectors := make(map[string]string)
	if labelMatch == "" {
		return routeSelectors, nil
	}
	for _, labelMatch := range strings.Split(labelMatch, ",") {
		if !strings.Contains(labelMatch, "=") {
			return nil, fmt.Errorf("Expected key=value format for label-match")
		}
		tokens := strings.Split(labelMatch, "=")
		routeSelectors[strings.TrimSpace(tokens[0])] = strings.TrimSpace(tokens[1])
	}
	return routeSelectors, nil
}
