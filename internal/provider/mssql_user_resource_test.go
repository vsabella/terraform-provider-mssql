// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccMssqlUserResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: providerConfig + testAccMssqlUserResourceConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mssql_user.test", "username", "testusername"),
					resource.TestCheckResourceAttr("mssql_user.test", "password", "testpassword-meet-requirements1234@@@"),
					resource.TestCheckResourceAttr("mssql_user.test", "external", "false"),
					resource.TestCheckResourceAttr("mssql_user.test", "default_schema", "dbo"),
				),
			},
			// ImportState testing
			//{
			//	Config:            providerConfig,
			//	ResourceName:      "mssql_MssqlUser.test",
			//	ImportState:       true,
			//	ImportStateVerify: true,
			//	// This is not normally necessary, but is here because this
			//	// MssqlUser code does not have an actual upstream service.
			//	// Once the Read method is able to refresh information from
			//	// the upstream service, this can be removed.
			//	ImportStateVerifyIgnore: []string{"configurable_attribute", "defaulted"},
			//},
			//// Update and Read testing
			//{
			//	Config: providerConfig + testAccMssqlUserResourceConfig(),
			//	Check: resource.ComposeAggregateTestCheckFunc(
			//		resource.TestCheckResourceAttr("mssql_MssqlUser.test", "username", "testusername"),
			//		resource.TestCheckResourceAttr("mssql_MssqlUser.test", "password", "testpassword"),
			//		resource.TestCheckResourceAttr("mssql_MssqlUser.test", "external", "false"),
			//		resource.TestCheckResourceAttr("mssql_MssqlUser.test", "default_schema", "dbo"),
			//	),
			//},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccMssqlUserResourceConfig() string {
	return `
resource "mssql_user" "test" {
  username = "testusername"
  password = "testpassword-meet-requirements1234@@@"
}
`
}
