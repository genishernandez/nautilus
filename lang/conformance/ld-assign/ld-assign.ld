PROGRAM AssignLd
(* Two chained assignments behind A, then B, then the coil: the assignments
   are made when A passes power, and the coil sees A AND B. *)
VAR_EXTERNAL
    A    : BOOL;
    B    : BOOL;
    X    : INT;
    ld_Y : INT;
    ld_N : INT;
    ld_P : BOOL;
END_VAR
LD
  RUNG asg
    A { ld_Y := X; ld_N := ld_N + 1 } B ( ld_P )
END_LD
END_PROGRAM
