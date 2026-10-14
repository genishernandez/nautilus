PROGRAM MainProgram
(* An array of timers called by a literal index and by a computed index in
   one program, beside a counter array called by literal index only. Every
   literal-index element carries its preset in the tag's data; the
   computed-index call takes a MOVE to its .PRE, and because that MOVE is
   sticky on the controller, a literal-index call on the same array
   re-asserts its own preset with a MOVE too. *)
VAR_EXTERNAL
    Pump1Run  : BOOL;
    Valve1Open : BOOL;
    TankLevelHi : BOOL;
    Pump1Ready : BOOL;
    Valve1Done : BOOL;
    Sel       : DINT;
END_VAR
VAR
    Delays : ARRAY [0..3] OF TON;
    Starts : ARRAY [0..1] OF CTU;
END_VAR
LD
  RUNG r0
    Pump1Run Delays[0]:TON(PT := T#2S) ( Pump1Ready )
  RUNG r1
    Valve1Open Delays[3]:TON(PT := T#500MS) ( Valve1Done )
  RUNG r2
    TankLevelHi Delays[Sel]:TON(PT := T#1S)
  RUNG r3
    +Pump1Run Starts[1]:CTU(PV := 3)
END_LD
END_PROGRAM
