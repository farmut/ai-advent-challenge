package petstore

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// ---------------------------------------------------------------------------
// Tool schema types
// ---------------------------------------------------------------------------

// Tool is the MCP tool descriptor returned by tools/list.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// prop is a shorthand for building JSON-Schema property maps.
type prop = map[string]interface{}

// ---------------------------------------------------------------------------
// Tool definitions
// ---------------------------------------------------------------------------

// ToolDefinitions returns the complete list of MCP tools exposed by this server.
// Each tool maps 1-to-1 to a Swagger Petstore API operation.
func ToolDefinitions() []Tool {
	return []Tool{

		// ── Pet ──────────────────────────────────────────────────────────────

		{
			Name:        "pet_add",
			Description: "Add a new pet to the store. Requires name and at least one photo URL.",
			InputSchema: schema(
				prop{
					"name":      strProp("Pet name"),
					"photoUrls": arrStrProp("List of photo URLs"),
					"id":        intProp("Pet ID (optional, server assigns one if omitted)"),
					"category":  objProp("Category object with 'id' (integer) and 'name' (string) fields"),
					"tags":      arrObjProp("List of tag objects, each with 'id' (integer) and 'name' (string) fields"),
					"status":    enumProp("Pet status in the store", "available", "pending", "sold"),
				},
				"name", "photoUrls",
			),
		},

		{
			Name:        "pet_update",
			Description: "Update an existing pet. The pet must already exist (identified by its id field).",
			InputSchema: schema(
				prop{
					"id":        intProp("Pet ID (required to identify the pet)"),
					"name":      strProp("Pet name"),
					"photoUrls": arrStrProp("List of photo URLs"),
					"category":  objProp("Category object with 'id' (integer) and 'name' (string) fields"),
					"tags":      arrObjProp("List of tag objects, each with 'id' (integer) and 'name' (string) fields"),
					"status":    enumProp("Pet status in the store", "available", "pending", "sold"),
				},
				"id", "name", "photoUrls",
			),
		},

		{
			Name:        "pet_find_by_status",
			Description: "Find pets by status. Returns a list of pets matching any of the given statuses.",
			InputSchema: schema(
				prop{
					"status": prop{
						"type":        "array",
						"description": "One or more status values to filter by",
						"items":       prop{"type": "string", "enum": []string{"available", "pending", "sold"}},
					},
				},
				"status",
			),
		},

		{
			Name:        "pet_find_by_tags",
			Description: "Find pets by tags. Returns a list of pets that have any of the given tags.",
			InputSchema: schema(
				prop{
					"tags": arrStrProp("List of tag names to filter by"),
				},
				"tags",
			),
		},

		{
			Name:        "pet_get_by_id",
			Description: "Find a single pet by its numeric ID. Returns the full pet record.",
			InputSchema: schema(
				prop{
					"petId": intProp("ID of the pet to retrieve"),
				},
				"petId",
			),
		},

		{
			Name:        "pet_update_with_form",
			Description: "Update name and/or status of an existing pet using form data.",
			InputSchema: schema(
				prop{
					"petId":  intProp("ID of the pet to update"),
					"name":   strProp("New name for the pet (optional)"),
					"status": strProp("New status for the pet (optional)"),
				},
				"petId",
			),
		},

		{
			Name:        "pet_delete",
			Description: "Delete a pet from the store by its numeric ID.",
			InputSchema: schema(
				prop{
					"petId": intProp("ID of the pet to delete"),
				},
				"petId",
			),
		},

		// ── Store ─────────────────────────────────────────────────────────────

		{
			Name:        "store_get_inventory",
			Description: "Return a map of pet status codes to quantities (inventory snapshot).",
			InputSchema: schema(prop{}, /* no params */),
		},

		{
			Name:        "store_place_order",
			Description: "Place a purchase order for a pet.",
			InputSchema: schema(
				prop{
					"petId":    intProp("ID of the pet being ordered"),
					"quantity": intProp("Number of pets to order"),
					"id":       intProp("Order ID (optional)"),
					"shipDate": strProp("Estimated ship date in ISO-8601 format, e.g. 2024-12-31T00:00:00Z (optional)"),
					"status":   enumProp("Order status", "placed", "approved", "delivered"),
					"complete": prop{"type": "boolean", "description": "Whether the order is complete"},
				},
				/* no required fields — server accepts partial orders */
			),
		},

		{
			Name:        "store_get_order",
			Description: "Find a purchase order by its ID. Valid IDs are integers between 1 and 10.",
			InputSchema: schema(
				prop{
					"orderId": prop{"type": "integer", "description": "Order ID (1–10)", "minimum": 1, "maximum": 10},
				},
				"orderId",
			),
		},

		{
			Name:        "store_delete_order",
			Description: "Delete a purchase order by its ID.",
			InputSchema: schema(
				prop{
					"orderId": intProp("ID of the order to delete (positive integer)"),
				},
				"orderId",
			),
		},

		// ── User ──────────────────────────────────────────────────────────────

		{
			Name:        "user_create",
			Description: "Create a new user account.",
			InputSchema: schema(
				prop{
					"id":         intProp("User ID (optional)"),
					"username":   strProp("Username"),
					"firstName":  strProp("First name"),
					"lastName":   strProp("Last name"),
					"email":      strProp("Email address"),
					"password":   strProp("Password"),
					"phone":      strProp("Phone number"),
					"userStatus": intProp("User status (integer flag)"),
				},
				"username",
			),
		},

		{
			Name:        "user_create_with_list",
			Description: "Create multiple user accounts from a list of user objects in one request.",
			InputSchema: schema(
				prop{
					"users": prop{
						"type":        "array",
						"description": "List of user objects to create",
						"items": prop{
							"type": "object",
							"properties": prop{
								"id":         intProp("User ID"),
								"username":   strProp("Username"),
								"firstName":  strProp("First name"),
								"lastName":   strProp("Last name"),
								"email":      strProp("Email address"),
								"password":   strProp("Password"),
								"phone":      strProp("Phone number"),
								"userStatus": intProp("User status"),
							},
						},
					},
				},
				"users",
			),
		},

		{
			Name:        "user_login",
			Description: "Log a user into the system and retrieve a session token.",
			InputSchema: schema(
				prop{
					"username": strProp("The username for login"),
					"password": strProp("The password in clear text"),
				},
				"username", "password",
			),
		},

		{
			Name:        "user_logout",
			Description: "Log out the current logged-in user session.",
			InputSchema: schema(prop{} /* no params */),
		},

		{
			Name:        "user_get",
			Description: "Get a user by their username. Use 'user1' for a known test account.",
			InputSchema: schema(
				prop{
					"username": strProp("Username to look up"),
				},
				"username",
			),
		},

		{
			Name:        "user_update",
			Description: "Update an existing user account by username.",
			InputSchema: schema(
				prop{
					"username":   strProp("Username identifying the account to update"),
					"id":         intProp("User ID (optional)"),
					"firstName":  strProp("New first name"),
					"lastName":   strProp("New last name"),
					"email":      strProp("New email address"),
					"password":   strProp("New password"),
					"phone":      strProp("New phone number"),
					"userStatus": intProp("New user status"),
				},
				"username",
			),
		},

		{
			Name:        "user_delete",
			Description: "Delete a user account by username.",
			InputSchema: schema(
				prop{
					"username": strProp("Username of the account to delete"),
				},
				"username",
			),
		},

		// ── Reports ───────────────────────────────────────────────────────────

		{
			Name: "report_start_collection",
			Description: "Start a background process that periodically fetches all pets with status=sold " +
				"and appends timestamped snapshots to a JSON report file. " +
				"The first snapshot is taken immediately; subsequent ones follow every interval_seconds seconds. " +
				"Collection runs independently of the agent — the agent does not need to stay connected. " +
				"NOTE: requires the MCP server to run in persistent HTTP+SSE mode (-addr flag); " +
				"in stdio mode the collection stops when the tool call ends. " +
				"Returns an error if a collection is already running — call report_stop_collection first.",
			InputSchema: schema(
				prop{
					"report_file":      strProp("Path to the JSON report file where snapshots will be stored"),
					"interval_seconds": intProp("Interval between snapshots in seconds (minimum 1)"),
				},
				"report_file", "interval_seconds",
			),
		},

		{
			Name:        "report_stop_collection",
			Description: "Stop the background collection started by report_start_collection. Returns a summary of how many snapshots were collected and when the last one was taken.",
			InputSchema: schema(prop{} /* no parameters */),
		},

		{
			Name:        "report_collection_status",
			Description: "Return the current state of the background collector: whether it is running, the collection interval, the report file path, the number of snapshots collected this run, and when the last snapshot was taken.",
			InputSchema: schema(prop{} /* no parameters */),
		},

		{
			Name: "report_show_sold",
			Description: "Read the sold-pets report from a JSON file written by the background collector " +
				"and return its full contents with a human-readable summary (snapshot count, latest reading).",
			InputSchema: schema(
				prop{
					"report_file": strProp("Path to the JSON report file created by report_start_collection"),
				},
				"report_file",
			),
		},
	}
}

// ---------------------------------------------------------------------------
// Tool dispatch
// ---------------------------------------------------------------------------

// CallTool executes the named tool with the given arguments and returns a
// human-readable text result (pretty-printed JSON from the API response).
func CallTool(c *Client, name string, args map[string]interface{}) (string, error) {
	switch name {

	// ── Pet ──────────────────────────────────────────────────────────────────

	case "pet_add":
		body := petBody(args)
		return callJSON(c.Post("/pet", body))

	case "pet_update":
		body := petBody(args)
		return callJSON(c.Put("/pet", body))

	case "pet_find_by_status":
		q := url.Values{}
		if statuses, ok := args["status"].([]interface{}); ok {
			for _, s := range statuses {
				if str, ok := s.(string); ok {
					q.Add("status", str)
				}
			}
		}
		return callJSON(c.Get("/pet/findByStatus", q))

	case "pet_find_by_tags":
		q := url.Values{}
		if tags, ok := args["tags"].([]interface{}); ok {
			for _, t := range tags {
				if str, ok := t.(string); ok {
					q.Add("tags", str)
				}
			}
		}
		return callJSON(c.Get("/pet/findByTags", q))

	case "pet_get_by_id":
		id, err := intArg(args, "petId")
		if err != nil {
			return "", err
		}
		return callJSON(c.Get(fmt.Sprintf("/pet/%d", id), nil))

	case "pet_update_with_form":
		id, err := intArg(args, "petId")
		if err != nil {
			return "", err
		}
		form := url.Values{}
		if v, ok := args["name"].(string); ok && v != "" {
			form.Set("name", v)
		}
		if v, ok := args["status"].(string); ok && v != "" {
			form.Set("status", v)
		}
		return callJSON(c.PostForm(fmt.Sprintf("/pet/%d", id), form))

	case "pet_delete":
		id, err := intArg(args, "petId")
		if err != nil {
			return "", err
		}
		return callJSON(c.Delete(fmt.Sprintf("/pet/%d", id)))

	// ── Store ─────────────────────────────────────────────────────────────────

	case "store_get_inventory":
		return callJSON(c.Get("/store/inventory", nil))

	case "store_place_order":
		body := orderBody(args)
		return callJSON(c.Post("/store/order", body))

	case "store_get_order":
		id, err := intArg(args, "orderId")
		if err != nil {
			return "", err
		}
		return callJSON(c.Get(fmt.Sprintf("/store/order/%d", id), nil))

	case "store_delete_order":
		id, err := intArg(args, "orderId")
		if err != nil {
			return "", err
		}
		return callJSON(c.Delete(fmt.Sprintf("/store/order/%d", id)))

	// ── User ──────────────────────────────────────────────────────────────────

	case "user_create":
		body := userBody(args)
		return callJSON(c.Post("/user", body))

	case "user_create_with_list":
		rawUsers, ok := args["users"].([]interface{})
		if !ok {
			return "", fmt.Errorf("'users' must be an array of user objects")
		}
		return callJSON(c.Post("/user/createWithList", rawUsers))

	case "user_login":
		uname, ok := args["username"].(string)
		if !ok || uname == "" {
			return "", fmt.Errorf("'username' is required")
		}
		pw, ok := args["password"].(string)
		if !ok || pw == "" {
			return "", fmt.Errorf("'password' is required")
		}
		q := url.Values{"username": {uname}, "password": {pw}}
		return callJSON(c.Get("/user/login", q))

	case "user_logout":
		return callJSON(c.Get("/user/logout", nil))

	case "user_get":
		uname, ok := args["username"].(string)
		if !ok || uname == "" {
			return "", fmt.Errorf("'username' is required")
		}
		return callJSON(c.Get("/user/"+uname, nil))

	case "user_update":
		uname, ok := args["username"].(string)
		if !ok || uname == "" {
			return "", fmt.Errorf("'username' is required")
		}
		body := userBody(args)
		return callJSON(c.Put("/user/"+uname, body))

	case "user_delete":
		uname, ok := args["username"].(string)
		if !ok || uname == "" {
			return "", fmt.Errorf("'username' is required")
		}
		return callJSON(c.Delete("/user/" + uname))

	// ── Reports ───────────────────────────────────────────────────────────────
	// report_start_collection / report_stop_collection / report_collection_status
	// are handled by Handler.CallTool (they need the Collector).

	case "report_show_sold":
		reportFile, err := strArg(args, "report_file")
		if err != nil {
			return "", err
		}
		return ShowSoldReport(reportFile)

	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// ---------------------------------------------------------------------------
// Helpers — argument extraction
// ---------------------------------------------------------------------------

func strArg(args map[string]interface{}, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("'%s' is required", key)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("'%s' must be a non-empty string", key)
	}
	return s, nil
}

func intArg(args map[string]interface{}, key string) (int64, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("'%s' is required", key)
	}
	switch n := v.(type) {
	case float64:
		return int64(n), nil
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("'%s' must be an integer, got %q", key, n)
		}
		return i, nil
	}
	return 0, fmt.Errorf("'%s' must be an integer, got %T", key, v)
}

// callJSON pretty-prints the raw JSON response from the API.
func callJSON(data []byte, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "(empty response — operation succeeded)", nil
	}
	var pretty interface{}
	if jsonErr := json.Unmarshal(data, &pretty); jsonErr != nil {
		// Not JSON (e.g. login returns a plain string)
		return string(data), nil
	}
	out, _ := json.MarshalIndent(pretty, "", "  ")
	return string(out), nil
}

// ---------------------------------------------------------------------------
// Helpers — body builders
// ---------------------------------------------------------------------------

func petBody(args map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{}
	if v, ok := args["id"]; ok {
		body["id"] = toInt64(v)
	}
	if v, ok := args["name"].(string); ok {
		body["name"] = v
	}
	if v, ok := args["photoUrls"]; ok {
		body["photoUrls"] = v
	}
	if v, ok := args["category"]; ok {
		body["category"] = v
	}
	if v, ok := args["tags"]; ok {
		body["tags"] = v
	}
	if v, ok := args["status"].(string); ok {
		body["status"] = v
	}
	return body
}

func orderBody(args map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{}
	if v, ok := args["id"]; ok {
		body["id"] = toInt64(v)
	}
	if v, ok := args["petId"]; ok {
		body["petId"] = toInt64(v)
	}
	if v, ok := args["quantity"]; ok {
		body["quantity"] = toInt64(v)
	}
	if v, ok := args["shipDate"].(string); ok {
		body["shipDate"] = v
	}
	if v, ok := args["status"].(string); ok {
		body["status"] = v
	}
	if v, ok := args["complete"]; ok {
		body["complete"] = v
	}
	return body
}

func userBody(args map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{}
	for _, key := range []string{"id", "userStatus"} {
		if v, ok := args[key]; ok {
			body[key] = toInt64(v)
		}
	}
	for _, key := range []string{"username", "firstName", "lastName", "email", "password", "phone"} {
		if v, ok := args[key].(string); ok {
			body[key] = v
		}
	}
	return body
}

func toInt64(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

// ---------------------------------------------------------------------------
// Helpers — JSON-Schema builders
// ---------------------------------------------------------------------------

func schema(properties prop, required ...string) prop {
	s := prop{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) prop {
	return prop{"type": "string", "description": desc}
}

func intProp(desc string) prop {
	return prop{"type": "integer", "description": desc}
}

func objProp(desc string) prop {
	return prop{"type": "object", "description": desc}
}

func arrStrProp(desc string) prop {
	return prop{
		"type":        "array",
		"description": desc,
		"items":       prop{"type": "string"},
	}
}

func arrObjProp(desc string) prop {
	return prop{
		"type":        "array",
		"description": desc,
		"items":       prop{"type": "object"},
	}
}

func enumProp(desc string, values ...string) prop {
	iface := make([]interface{}, len(values))
	for i, v := range values {
		iface[i] = v
	}
	return prop{"type": "string", "description": desc, "enum": iface}
}
