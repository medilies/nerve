# Core of nerve

## Vision

- simplicity over complex useful feature
- Progress with no bugs and no regressions

## Goal

## A remote brain repo is the source of truth

- It has my personal skills files
- non sensitive setting per project and common for all projects
- It references 3rd party skills with hashes
- hooks, plugins...
- all in a toml file

## claude ai

no support at the moment

## a cli tool

Everything it does is explicit and returns understandable output

- push from a project overrides the brain
- sync override the project with what is on the brain
- update looks for latest third party skill hashes and updates them in current project and the brain
- All conflict fails and return a failure message with diff
- -f ignores conflicts
- empty settings or folders are not deleted when left empty
- remote and local brain toml always sync

## a skill

all the complexity of the cli is hidden behind the skill that is why good output is needed

## Resolving problems

- consider fixes and dropping the causing feature if it derails from the core.
- Interesting ideas and dropped features get backlogged
