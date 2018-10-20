# Team takedashi ISUCON8 Final

## Ansible

*with password*

```
ansible-playbook -u isucon -c paramiko -kKs -i hosts site.yml
```

*without password*

```
ansible-playbook -u isucon -c paramiko -s -i hosts site.yml
```
