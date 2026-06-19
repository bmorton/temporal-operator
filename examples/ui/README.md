# Operator UI example

The operator serves a read-only UI when you set `--ui-bind-address` (e.g.
`:8082`). It is disabled by default. The UI performs no authentication itself;
front it with a forward-auth proxy such as Authelia and pass the trusted
identity headers through.

1. Enable the UI on the manager by adding `--ui-bind-address=:8082` to its
   arguments. With the Helm chart that means appending it to `manager.args` in
   your values (alongside `--leader-elect`); with raw kustomize, add it to the
   manager container args.
2. Apply `config/ui` to expose the `operator-ui` Service.
3. Apply `ingress-authelia.yaml` (edit hosts/URLs) so Authelia authenticates
   users and injects `Remote-User` / `Remote-Groups` / `Remote-Email`.
4. Set `--ui-require-auth` whenever the Service is exposed so the operator
   returns 401 for requests without the trusted user header (fail closed).

## Security

Forward-auth on the Ingress only protects requests that arrive through that
Ingress. The `operator-ui` Service still routes directly to the manager pod on
port 8082, so direct in-cluster access (including `kubectl port-forward
svc/operator-ui 8082`) bypasses Authelia unless the manager also enforces
`--ui-require-auth`.

When you expose the UI, set `--ui-require-auth` on the manager so requests
lacking a trusted user header fail closed with 401, including direct Service
access that did not pass through the proxy.

You can also apply `networkpolicy.yaml` to restrict access to your ingress
controller namespace; edit the selectors to match your environment.

TLS terminates at the Ingress. The pod serves plain HTTP, so do not expose port
8082 outside the cluster without a TLS-terminating proxy.
