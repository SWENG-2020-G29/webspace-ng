/*
 * Netsoc webspaced
 *
 * API for managing next-gen webspaces. 
 *
 * API version: 1.2.0
 * Generated by: OpenAPI Generator (https://openapi-generator.tech)
 */

package webspaced
// ExecInteractiveControl struct for ExecInteractiveControl
type ExecInteractiveControl struct {
	Resize ResizeRequest `json:"resize,omitempty"`
	// Signal to send to process
	Signal int32 `json:"signal,omitempty"`
}
