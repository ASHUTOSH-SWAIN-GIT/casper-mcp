Use the casper MCP tools to build infrastructure context for this project.

$ARGUMENTS

Instructions:
- If a specific intent or resource was provided in $ARGUMENTS, call get_context with that intent to find relevant resources, dependencies, and examples.
- If no intent was provided, call dump_graph to get a full snapshot of the infrastructure graph, then summarise: total resources, resource types, and any policy violations.
- After getting context, briefly describe what you found — resource names, types, dependencies, and anything notable (drift, policy violations, workflow decisions).
- If the user wants to make a change, call simulate_impact with the proposed HCL before applying anything.
