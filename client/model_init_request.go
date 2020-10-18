/*
 * Netsoc webspaced
 *
 * API for managing next-gen webspaces. 
 *
 * API version: 1.1.2
 * Generated by: OpenAPI Generator (https://openapi-generator.tech)
 */

package webspaced
// InitRequest struct for InitRequest
type InitRequest struct {
	// Image alias or fingerprint
	Image string `json:"image"`
	// Password for root user
	Password string `json:"password,omitempty"`
	// SSH key for root user
	SshKey string `json:"sshKey,omitempty"`
}
