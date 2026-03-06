APP_NAME := clawcolony
IMAGE ?= clawcolony:dev
RUNTIME_NS ?= freewill
BOT_NS ?= freewill
USER_NS ?= $(BOT_NS)

.PHONY: run build docker-build minikube-load deploy undeploy test check-doc

run:
	go run ./cmd/clawcolony

build:
	go build -o bin/$(APP_NAME) ./cmd/clawcolony

docker-build:
	docker build -t $(IMAGE) .

minikube-load:
	minikube image load $(IMAGE)

deploy:
	kubectl apply -f k8s/rbac.yaml
	sed 's/{{CLAWCOLONY_IMAGE}}/$(IMAGE)/g' k8s/clawcolony-runtime-deployment.yaml | kubectl apply -f -
	kubectl apply -f k8s/service-runtime.yaml

undeploy:
	kubectl -n $(RUNTIME_NS) delete svc clawcolony --ignore-not-found
	kubectl -n $(RUNTIME_NS) delete deploy clawcolony-runtime --ignore-not-found
	kubectl -n $(RUNTIME_NS) delete sa clawcolony-runtime-sa --ignore-not-found
	kubectl -n $(RUNTIME_NS) delete role clawcolony-runtime-self-role --ignore-not-found
	kubectl -n $(USER_NS) delete role clawcolony-runtime-user-role --ignore-not-found
	kubectl -n $(RUNTIME_NS) delete rolebinding clawcolony-runtime-self-binding --ignore-not-found
	kubectl -n $(USER_NS) delete rolebinding clawcolony-runtime-user-binding --ignore-not-found

test:
	go test ./...

check-doc:
	./scripts/check_doc_update.sh
