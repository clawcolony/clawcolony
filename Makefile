APP_NAME := clawcolony
IMAGE ?= clawcolony:dev
CLAWCOLONY_NS ?= clawcolony
BOT_NS ?= freewill
USER_NS ?= $(BOT_NS)

.PHONY: run build docker-build minikube-load deploy undeploy test check-doc bootstrap-full genesis-regression genesis-real-smoke genesis-real-stress genesis-dialog-smoke genesis-dialog-stress genesis-verify

run:
	go run ./cmd/clawcolony

build:
	go build -o bin/$(APP_NAME) ./cmd/clawcolony

docker-build:
	docker build -t $(IMAGE) .

minikube-load:
	minikube image load $(IMAGE)

deploy:
	kubectl apply -f k8s/namespaces.yaml
	kubectl apply -f k8s/nats.yaml
	kubectl apply -f k8s/postgres.yaml
	kubectl apply -f k8s/rbac.yaml
	sed 's/{{CLAWCOLONY_IMAGE}}/$(IMAGE)/g' k8s/clawcolony-deployment.yaml | kubectl apply -f -
	kubectl apply -f k8s/service.yaml

undeploy:
	kubectl -n $(CLAWCOLONY_NS) delete svc clawcolony --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete deploy clawcolony --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete sa clawcolony-sa --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete role clawcolony-self-role --ignore-not-found
	kubectl -n $(USER_NS) delete role clawcolony-user-role --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete rolebinding clawcolony-self-binding --ignore-not-found
	kubectl -n $(USER_NS) delete rolebinding clawcolony-user-binding --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete svc clawcolony-postgres --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete statefulset clawcolony-postgres --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete secret clawcolony-postgres --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete pvc pgdata-clawcolony-postgres-0 --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete svc clawcolony-nats --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete statefulset clawcolony-nats --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete configmap clawcolony-nats-config --ignore-not-found
	kubectl -n $(CLAWCOLONY_NS) delete pvc nats-data-clawcolony-nats-0 --ignore-not-found
	# namespaces are preserved by default

test:
	go test ./...

check-doc:
	./scripts/check_doc_update.sh

bootstrap-full:
	./scripts/bootstrap_full_stack.sh

genesis-regression:
	./scripts/genesis_robustness_regression.sh

genesis-real-smoke:
	./scripts/genesis_real_agents_smoke.sh

genesis-real-stress:
	./scripts/genesis_real_agents_stress.sh

genesis-dialog-smoke:
	./scripts/genesis_real_agent_dialog_actions.sh

genesis-dialog-stress:
	./scripts/genesis_real_agent_dialog_stress.sh

genesis-verify:
	./scripts/genesis_robustness_regression.sh
	./scripts/genesis_real_agents_smoke.sh
