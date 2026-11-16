# nerve

A Brain, and the Nervous system that carries it to each place an agent runs.

## Language

**Brain**:
One person's repo of everything that shapes how their agents work: own skills, third-party pins, settings and standing instructions.
_Avoid_: config, dotfiles, kit, agentic brain

**Nervous system**:
The tool and its skill that carry any Brain to its surfaces and write changes back into it; it knows nothing about whose Brain it carries.
_Avoid_: sync tool, sync script, engine

**Skill**:
A folder of instructions (`SKILL.md` plus optional files) that an agent loads when a task matches its description.

**Own skill**:
A skill whose content is written and kept in a Brain.
_Avoid_: my skill, custom skill, local skill

**Third-party skill**:
A skill written in someone else's repo and used in a Brain at a pinned commit.
_Avoid_: external skill, vendored skill

**Plugin**:
A named set of skills that Claude installs from a Brain and invokes under the plugin's name; a Brain's own skills form one plugin, and each third-party skill is a plugin of its own.
_Avoid_: package, bundle

**Pin**:
The exact commit a third-party skill, or the Nervous system itself, is used at.
_Avoid_: version, lock

**Surface**:
A place where skills are used: one Claude Code config, or one claude.ai account.
_Avoid_: target, agent, platform

**Profile**:
A named set of settings and instructions for one kind of use, such as work or personal; each Claude Code config on a machine follows one profile.
_Avoid_: environment, flavour, account

**Local overlay**:
Settings kept on one machine and never in the Brain, for anything sensitive or identifying.
_Avoid_: secrets file, private settings

**Drift**:
A difference between what a surface holds and what the Brain says it should hold.
_Avoid_: out of sync, divergence
