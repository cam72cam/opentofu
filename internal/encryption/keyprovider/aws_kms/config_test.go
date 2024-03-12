package aws_kms

import (
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/davecgh/go-spew/spew"
	awsbase "github.com/hashicorp/aws-sdk-go-base/v2"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/opentofu/opentofu/internal/gohcl"
	"github.com/opentofu/opentofu/internal/httpclient"
	"github.com/opentofu/opentofu/version"
)

func TestConfig_asAWSBase(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected awsbase.Config
	}{
		{
			name: "minconfig",
			input: `
				kms_key_id = "my-kms-key-id"
				key_spec = "AES_256a"
				region = "magic-mountain"`,
			expected: awsbase.Config{
				Region:                 "magic-mountain",
				CallerDocumentationURL: "https://opentofu.org/docs/language/settings/backends/s3",
				CallerName:             "KMS Key Provider",
				MaxRetries:             5,
				UserAgent: awsbase.UserAgentProducts{
					{Name: "APN", Version: "1.0"},
					{Name: httpclient.DefaultApplicationName, Version: version.String()},
				},
			},
		},
		{
			name: "maxconfig",
			input: `
				kms_key_id = "my-kms-key-id"
				key_spec = "AES_256"

				access_key = "my-access-key"
				endpoints {
					iam = "endpoint-iam"
					sts = "endpoint-sts"
				}
				max_retries = 42
				profile = "my-profile"
				region = "my-region"
				secret_key = "my-secret-key"
				skip_credentials_validation = true
				skip_requesting_account_id = true
				sts_region = "my-sts-region"
				token = "my-token"
				http_proxy = "my-http-proxy"
				https_proxy = "my-https-proxy"
				no_proxy = "my-noproxy"
				insecure = true
				use_dualstack_endpoint = true
				use_fips_endpoint = true
				custom_ca_bundle = "my-custom-ca-bundle"
				ec2_metadata_service_endpoint = "my-emde"
				ec2_metadata_service_endpoint_mode = "my-emde-mode"
				skip_metadata_api_check = false
				shared_credentials_files = ["my-scredf"]
				shared_config_files = ["my-sconff"]
				assume_role = {
					role_arn = "ar_arn"
					duration = "4h"
					external_id = "ar_extid"
					policy = "ar_policy"
					policy_arns = ["arn:aws:iam::123456789012:policy/AR"]
					session_name = "ar_session_name"
					tags = {
						foo = "bar"
					}
					transitive_tag_keys = ["ar_tags"]
				}
				assume_role_with_web_identity = {
					role_arn = "wi_arn"
					duration = "5h"
					policy = "wi_policy"
					policy_arns = ["arn:aws:iam::123456789012:policy/WI"]
					session_name = "wi_session_name"
					web_identity_token = "wi_token"
					//web_identity_token_file = "wi_token_file"
				}
				allowed_account_ids = ["account"]
				//forbidden_account_ids = ?
				retry_mode = "adaptive"
				`,
			expected: awsbase.Config{
				CallerDocumentationURL: "https://opentofu.org/docs/language/settings/backends/s3",
				CallerName:             "KMS Key Provider",
				UserAgent: awsbase.UserAgentProducts{
					{Name: "APN", Version: "1.0"},
					{Name: httpclient.DefaultApplicationName, Version: version.String()},
				},

				AccessKey:                      "my-access-key",
				IamEndpoint:                    "https://endpoint-iam",
				MaxRetries:                     42,
				Profile:                        "my-profile",
				Region:                         "my-region",
				SecretKey:                      "my-secret-key",
				SkipCredsValidation:            true,
				SkipRequestingAccountId:        true,
				StsEndpoint:                    "https://endpoint-sts",
				StsRegion:                      "my-sts-region",
				Token:                          "my-token",
				HTTPProxy:                      aws.String("my-http-proxy"),
				HTTPSProxy:                     aws.String("my-https-proxy"),
				NoProxy:                        "my-noproxy",
				Insecure:                       true,
				UseDualStackEndpoint:           true,
				UseFIPSEndpoint:                true,
				CustomCABundle:                 "my-custom-ca-bundle",
				EC2MetadataServiceEnableState:  imds.ClientDisabled,
				EC2MetadataServiceEndpoint:     "my-emde",
				EC2MetadataServiceEndpointMode: "my-emde-mode",
				SharedCredentialsFiles:         []string{"my-scredf"},
				SharedConfigFiles:              []string{"my-sconff"},
				AssumeRole: &awsbase.AssumeRole{
					RoleARN:    "ar_arn",
					Duration:   time.Hour * 4,
					ExternalID: "ar_extid",
					Policy:     "ar_policy",
					PolicyARNs: []string{
						"arn:aws:iam::123456789012:policy/AR",
					},
					SessionName: "ar_session_name",
					Tags: map[string]string{
						"foo": "bar",
					},
					TransitiveTagKeys: []string{
						"ar_tags",
					},
				},
				AssumeRoleWithWebIdentity: &awsbase.AssumeRoleWithWebIdentity{
					RoleARN:  "wi_arn",
					Duration: time.Hour * 5,
					Policy:   "wi_policy",
					PolicyARNs: []string{
						"arn:aws:iam::123456789012:policy/WI",
					},
					SessionName:          "wi_session_name",
					WebIdentityToken:     "wi_token",
					WebIdentityTokenFile: "",
				},
				AllowedAccountIds: []string{"account"},
				RetryMode:         aws.RetryModeAdaptive,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input, diags := hclsyntax.ParseConfig([]byte(tc.input), "test", hcl.InitialPos)
			if diags.HasErrors() {
				t.Fatal(diags.Error())
			}

			config := new(Config)

			diags = gohcl.DecodeBody(input.Body, nil, config)
			if diags.HasErrors() {
				t.Fatal(diags.Error())
			}

			println(config.KeySpec.String())

			if config.KMSKeyID.Value != "my-kms-key-id" {
				t.Fatal("missing kms_key_id")
			}
			if config.KeySpec.Value != "AES_256" {
				t.Fatal("missing key_spec")
			}

			actual, err := config.asAWSBase()
			if err != nil {
				t.Fatal(err.Error())
			}
			if !reflect.DeepEqual(tc.expected, *actual) {
				t.Fatalf("Expected %s, got %s", spew.Sdump(tc.expected), spew.Sdump(*actual))
			}
		})
	}
}
