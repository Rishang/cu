FROM ghcr.io/astral-sh/uv:0.11.26 AS uv
FROM python:3.13-slim AS builder

COPY --from=uv /uv /uvx /usr/local/bin/
WORKDIR /opt/ansible

COPY ansible/pyproject.toml ansible/uv.lock ansible/requirements.yml ./
RUN uv sync --locked --no-dev --no-install-project \
    && .venv/bin/ansible-galaxy collection install -r requirements.yml -p collections \
    && rm -rf .venv/lib/python3.13/site-packages/ansible_test \
    && find .venv collections -type d \( -name tests -o -name __pycache__ \) -exec rm -rf {} + \
    && find .venv collections -type f -name '*.pyc' -delete

FROM python:3.13-slim
WORKDIR /opt/ansible

COPY --from=builder /opt/ansible/.venv .venv
COPY --from=builder /opt/ansible/collections collections
COPY ansible/roles roles

ENV PATH=/opt/ansible/.venv/bin:$PATH \
    ANSIBLE_ROLES_PATH=/opt/ansible/roles \
    ANSIBLE_COLLECTIONS_PATH=/opt/ansible/collections

ENTRYPOINT ["ansible-playbook"]
