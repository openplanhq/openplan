# openplan

A self-hosted platform where a platform team registers Terraform or OpenTofu
modules once, and anyone on the team can stand up and manage their own copy.

## Language

**Refusal**:
A permission decision saying no: the caller may not do this, or may not even see the thing they named.
_Avoid_: denial, rejection, failed access

**Rejection**:
Someone with permission being stopped by the state of things, such as a stale plan or a stack's last owner.
_Avoid_: refusal, failure
