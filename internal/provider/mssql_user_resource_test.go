// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
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
				Config: testAccMssqlUserResourceConfig("one"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("scaffolding_MssqlUser.test", "configurable_attribute", "one"),
					resource.TestCheckResourceAttr("scaffolding_MssqlUser.test", "defaulted", "MssqlUser value when not configured"),
					resource.TestCheckResourceAttr("scaffolding_MssqlUser.test", "id", "MssqlUser-id"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "scaffolding_MssqlUser.test",
				ImportState:       true,
				ImportStateVerify: true,
				// This is not normally necessary, but is here because this
				// MssqlUser code does not have an actual upstream service.
				// Once the Read method is able to refresh information from
				// the upstream service, this can be removed.
				ImportStateVerifyIgnore: []string{"configurable_attribute", "defaulted"},
			},
			// Update and Read testing
			{
				Config: testAccMssqlUserResourceConfig("two"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("scaffolding_MssqlUser.test", "configurable_attribute", "two"),
				),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccMssqlUserResourceConfig(configurableAttribute string) string {
	return fmt.Sprintf(`
resource "scaffolding_MssqlUser" "test" {
  configurable_attribute = %[1]q
}
`, configurableAttribute)
}
