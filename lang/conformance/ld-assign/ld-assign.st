PROGRAM AssignSt
(* The rung below, as ST: both assignments only while A; the coil is A AND B. *)
VAR_EXTERNAL
    A    : BOOL;
    B    : BOOL;
    X    : INT;
    st_Y : INT;
    st_N : INT;
    st_P : BOOL;
END_VAR

IF A THEN
    st_Y := X;
    st_N := st_N + 1;
END_IF;
st_P := A AND B;
END_PROGRAM
