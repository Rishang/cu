import typer
from .aws.cli import app as aws_app
from .azure.cli import app as azure_app
from .diff.cli import diff_cmd
from .k8s.cli import app as k8s_app
from .os_utils.cli import app as os_utils_app
from .task.cli import app as task_app
from .pwpush.cli import app as pwpush_app

app = typer.Typer(
    pretty_exceptions_enable=False,
)

app.add_typer(aws_app, name="aws", help="AWS-related commands")
app.add_typer(azure_app, name="az", help="Azure-related commands")
app.add_typer(os_utils_app, name="os", help="OS-related commands")
app.add_typer(k8s_app, name="k", help="Kubernetes-related commands")
app.add_typer(
    pwpush_app,
    name="pwpush",
    help="Password Pusher commands ref: https://docs.pwpush.com/",
)
app.command(
    "diff",
    help="Semantic diff for structured config files (JSON, YAML, TOML).",
)(diff_cmd)
app.command(
    "task",
    help="Taskfile commands",
    context_settings={"allow_extra_args": True, "ignore_unknown_options": True},
)(task_app)


def main():
    app()


if __name__ == "__main__":
    main()
