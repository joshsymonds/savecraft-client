# Section size exceptions

This is a closed, machine-checked list. It contains exactly two temporary
exceptions: `stellaris/species` and `satisfactory/production_lines`. Entries
may only retire after their rows/table follow-on work lands; the list must
never grow.

RimWorld is covered by its runnable C# partitioner tests rather than this
parser-fixture gate. Its partitioner uses accept-and-warn behavior: when one
unit cannot fit the byte budget, that single unfit unit is still emitted and
the condition is logged. It is not silently blessed as an exception.
