/*
Copyright (c) 2023 Red Hat, Inc.

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

package oidcconfig

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
	"github.com/zgalor/weberr"

	"github.com/openshift/rosa/cmd/dlt/oidcprovider"
	"github.com/openshift/rosa/pkg/arguments"
	"github.com/openshift/rosa/pkg/aws"
	awscb "github.com/openshift/rosa/pkg/aws/commandbuilder"
	"github.com/openshift/rosa/pkg/interactive"
	"github.com/openshift/rosa/pkg/interactive/confirm"
	"github.com/openshift/rosa/pkg/rosa"
)

var Cmd = &cobra.Command{
	Use:     "oidc-config",
	Aliases: []string{"oidconfig, oidcconfig"},
	Short:   "Delete OIDC Config",
	Long:    "Cleans up OIDC config based on secret ARN.",
	Example: `  # Delete OIDC config based on secret ARN that has been supplied
	rosa delete oidc-config --oidc-private-key-secret-arn <oidc_private_key_secret_arn>`,
	Hidden: true,
	Run:    run,
}

const (
	//nolint
	OidcPrivateKeySecretArnFlag = "oidc-private-key-secret-arn"
	redHatHostedFlag            = "rh-hosted"

	prefixForPrivateKeySecret = "rosa-private-key-"
	secretsManagerService     = "secretsmanager"
)

var args struct {
	oidcPrivateKeySecretArn string
	region                  string
	redHatHosted            bool
}

func init() {
	flags := Cmd.Flags()

	flags.StringVar(
		&args.oidcPrivateKeySecretArn,
		OidcPrivateKeySecretArnFlag,
		"",
		"AWS Secrets Manager ARN for identification of config",
	)

	flags.BoolVar(
		&args.redHatHosted,
		redHatHostedFlag,
		false,
		"Indicates whether it is a Red Hat hosted or Customer hosted OIDC Configuration.",
	)

	aws.AddModeFlag(Cmd)

	interactive.AddFlag(flags)
	confirm.AddFlag(flags)
}

func run(cmd *cobra.Command, argv []string) {
	r := rosa.NewRuntime().WithAWS().WithOCM()
	defer r.Cleanup()

	mode, err := aws.GetMode()
	if err != nil {
		r.Reporter.Errorf("%s", err)
		os.Exit(1)
	}

	if args.redHatHosted && mode != aws.ModeAuto {
		r.Reporter.Warnf("--rh-hosted param is not supported outside --mode auto flow.")
		os.Exit(1)
	}

	// Get AWS region
	region, err := aws.GetRegion(arguments.GetRegion())
	if err != nil {
		r.Reporter.Errorf("Error getting region: %v", err)
		os.Exit(1)
	}
	args.region = region

	// Determine if interactive mode is needed
	if !interactive.Enabled() && !cmd.Flags().Changed("mode") {
		interactive.Enable()
	}

	if interactive.Enabled() {
		mode, err = interactive.GetOption(interactive.Input{
			Question: "OIDC config creation mode",
			Help:     cmd.Flags().Lookup("mode").Usage,
			Default:  aws.ModeAuto,
			Options:  aws.Modes,
			Required: true,
		})
		if err != nil {
			r.Reporter.Errorf("Expected a valid OIDC provider creation mode: %s", err)
			os.Exit(1)
		}
	}

	oidcPrivateKeySecretArn := args.oidcPrivateKeySecretArn
	if oidcPrivateKeySecretArn == "" || interactive.Enabled() {
		oidcPrivateKeySecretArn, err = interactive.GetString(
			interactive.Input{
				Question: "OIDC Private Key Secret ARN",
				Help:     cmd.Flags().Lookup(OidcPrivateKeySecretArnFlag).Usage,
				Required: true,
				Default:  oidcPrivateKeySecretArn,
			})
		if err != nil {
			r.Reporter.Errorf("Expected a valid ARN to the secret containing the private key: %s", err)
			os.Exit(1)
		}
		err = aws.ARNValidator(oidcPrivateKeySecretArn)
		if err != nil {
			r.Reporter.Errorf("%s", err)
			os.Exit(1)
		}
		parsedSecretArn, _ := arn.Parse(oidcPrivateKeySecretArn)
		if parsedSecretArn.Service != secretsManagerService {
			r.Reporter.Errorf("Supplied secret ARN is not a valid Secrets Manager ARN")
			os.Exit(1)
		}
		args.oidcPrivateKeySecretArn = oidcPrivateKeySecretArn
	}

	oidcConfigInput := buildOidcConfigInput(r)
	oidcConfigStrategy, err := getOidcConfigStrategy(mode, oidcConfigInput)
	if err != nil {
		r.Reporter.Errorf("%s", err)
		os.Exit(1)
	}
	oidcConfigStrategy.execute(r)
	oidcprovider.Cmd.Run(oidcprovider.Cmd, []string{"", mode,
		fmt.Sprintf("https://%s.s3.%s.amazonaws.com", oidcConfigInput.BucketName, args.region)})
}

type OidcConfigInput struct {
	PrivateKeySecretArn string
	BucketName          string
}

func buildOidcConfigInput(r *rosa.Runtime) OidcConfigInput {
	parsedSecretArn, err := arn.Parse(args.oidcPrivateKeySecretArn)
	if err != nil {
		r.Reporter.Errorf("There was a problem parsing secret ARN '%s' : %v", args.oidcPrivateKeySecretArn, err)
		os.Exit(1)
	}
	if parsedSecretArn.Service != aws.SecretsManager {
		r.Reporter.Errorf("Supplied secret ARN is not a valid Secrets Manager ARN")
		os.Exit(1)
	}
	if args.region != parsedSecretArn.Region {
		r.Reporter.Errorf("Secret region '%s' differs from chosen region '%s', "+
			"please run the command supplying region parameter.", parsedSecretArn.Region, args.region)
		os.Exit(1)
	}
	secretResourceName, err := aws.GetResourceIdFromSecretArn(args.oidcPrivateKeySecretArn)
	if err != nil {
		r.Reporter.Errorf("There was a problem parsing secret ARN '%s' : %v", args.oidcPrivateKeySecretArn, err)
		os.Exit(1)
	}
	bucketName := strings.TrimPrefix(secretResourceName, prefixForPrivateKeySecret)
	if !args.redHatHosted {
		index := strings.LastIndex(bucketName, "-")
		if index != -1 {
			bucketName = bucketName[:index]
		}
	}
	hasClusterUsingOidcConfig, err := r.OCMClient.HasAClusterUsingOidcConfig(bucketName)
	if err != nil {
		r.Reporter.Errorf("There was a problem checking if any clusters are using OIDC config '%s' : %v", bucketName, err)
		os.Exit(1)
	}
	if hasClusterUsingOidcConfig {
		r.Reporter.Errorf("There are clusters using OIDC config '%s', can't delete the configuration", bucketName)
		os.Exit(1)
	}
	return OidcConfigInput{
		BucketName:          bucketName,
		PrivateKeySecretArn: args.oidcPrivateKeySecretArn,
	}
}

type DeleteOidcConfigStrategy interface {
	execute(r *rosa.Runtime)
}

type DeleteRedHatHostedOidcConfigAutoStrategy struct {
	oidcConfig OidcConfigInput
}

func (s *DeleteRedHatHostedOidcConfigAutoStrategy) execute(r *rosa.Runtime) {
	r.WithOCM()
	privateKeySecretArn := s.oidcConfig.PrivateKeySecretArn
	var spin *spinner.Spinner
	if r.Reporter.IsTerminal() {
		spin = spinner.New(spinner.CharSets[9], 100*time.Millisecond)
		r.Reporter.Infof("Deleting Red Hat hosted OIDC configuration")
	}
	if spin != nil {
		spin.Start()
	}
	err := r.OCMClient.DeleteRedHatHostedOidcConfig()
	if err != nil {
		r.Reporter.Errorf("There was a problem deleting Red Hat Hosted OIDC Configuration: %s", err)
		os.Exit(1)
	}
	err = r.AWSClient.DeleteSecretInSecretsManager(privateKeySecretArn)
	if err != nil {
		r.Reporter.Errorf("There was a problem deleting private key from secrets manager: %s", err)
		os.Exit(1)
	}
	if spin != nil {
		spin.Stop()
	}
	if r.Reporter.IsTerminal() {
		r.Reporter.Infof("Deleted OIDC configuration")
	}
}

type DeleteOidcConfigAutoStrategy struct {
	oidcConfig OidcConfigInput
}

func (s *DeleteOidcConfigAutoStrategy) execute(r *rosa.Runtime) {
	bucketName := s.oidcConfig.BucketName
	privateKeySecretArn := s.oidcConfig.PrivateKeySecretArn
	var spin *spinner.Spinner
	if r.Reporter.IsTerminal() {
		spin = spinner.New(spinner.CharSets[9], 100*time.Millisecond)
		r.Reporter.Infof("Deleting OIDC configuration '%s'", bucketName)
	}
	if spin != nil {
		spin.Start()
	}
	err := r.AWSClient.DeleteSecretInSecretsManager(privateKeySecretArn)
	if err != nil {
		r.Reporter.Errorf("There was a problem deleting private key from secrets manager: %s", err)
		os.Exit(1)
	}
	err = r.AWSClient.DeleteS3Bucket(bucketName)
	if err != nil {
		r.Reporter.Errorf("There was a problem deleting S3 bucket '%s': %s", bucketName, err)
		os.Exit(1)
	}
	if spin != nil {
		spin.Stop()
	}
	if r.Reporter.IsTerminal() {
		r.Reporter.Infof("Deleted OIDC configuration")
	}
}

type DeleteOidcConfigManualStrategy struct {
	oidcConfig OidcConfigInput
}

func (s *DeleteOidcConfigManualStrategy) execute(r *rosa.Runtime) {
	commands := []string{}
	bucketName := s.oidcConfig.BucketName
	privateKeySecretArn := s.oidcConfig.PrivateKeySecretArn
	deleteSecretCommand := awscb.NewSecretsManagerCommandBuilder().
		SetCommand(awscb.DeleteSecret).
		AddParam(awscb.SecretID, privateKeySecretArn).
		AddParam(awscb.Region, args.region).
		Build()
	commands = append(commands, deleteSecretCommand)
	emptyS3BucketCommand := awscb.NewS3CommandBuilder().
		SetCommand(awscb.Remove).
		AddValueNoParam(fmt.Sprintf("s3://%s", bucketName)).
		AddParamNoValue(awscb.Recursive).
		Build()
	commands = append(commands, emptyS3BucketCommand)
	deleteS3BucketCommand := awscb.NewS3CommandBuilder().
		SetCommand(awscb.RemoveBucket).
		AddValueNoParam(fmt.Sprintf("s3://%s", bucketName)).
		Build()
	commands = append(commands, deleteS3BucketCommand)
	fmt.Println(awscb.JoinCommands(commands))
}

func getOidcConfigStrategy(mode string, input OidcConfigInput) (DeleteOidcConfigStrategy, error) {
	if args.redHatHosted {
		return &DeleteRedHatHostedOidcConfigAutoStrategy{oidcConfig: input}, nil
	}
	switch mode {
	case aws.ModeAuto:
		return &DeleteOidcConfigAutoStrategy{oidcConfig: input}, nil
	case aws.ModeManual:
		return &DeleteOidcConfigManualStrategy{oidcConfig: input}, nil
	default:
		return nil, weberr.Errorf("Invalid mode. Allowed values are %s", aws.Modes)
	}
}
