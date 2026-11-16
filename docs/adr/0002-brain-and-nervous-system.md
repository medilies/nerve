# Brain and Nervous system as separate repos

Status: accepted, 2026-11-16

## Context

The sync tool started inside one person's skills repo and read that repo's layout. The repo is meant to grow into everything that shapes how their agents work: skills, stacks, settings and standing instructions, kept in step across projects and machines. The tool and the skill that drives it are useful to anyone with the same need; the content is not.

## Options

- **One repo** holding the tool and one person's content. Simplest, but anyone else must fork the content to get the tool, and every change to the tool lands among someone's habits.
- **Ship the opinionated skills with the tool** (bootstrap, testing, review). Gives newcomers a working setup, but makes the tool carry one person's way of working.
- **Two repos.** The Brain holds content only; the Nervous system (`nerve` and its one skill) carries any Brain and has no opinions.

## Decision

Two repos. The Nervous system knows a Brain's layout and nothing else: no stacks, no processes, no commit style. It ships exactly one skill, its own, embedded in the binary. Each Brain pins the Nervous system's version the way it pins a third-party skill, and `sync` brings the installed `nerve` to that pin.

## Consequences

- A Brain's layout is a contract the Nervous system must keep stable or migrate.
- A change to the Nervous system reaches a Brain only when that Brain moves its pin, so upgrades are reviewed.
- A new machine needs one manual install of `nerve` before the skill can take over.
