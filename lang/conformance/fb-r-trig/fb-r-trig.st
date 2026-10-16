PROGRAM RTrigSt
(* R_TRIG in Structured Text: the reference path. *)
VAR_EXTERNAL
    CLK  : BOOL;
    st_Q : BOOL;
END_VAR
VAR
    t : R_TRIG;
END_VAR

t(CLK := CLK);
st_Q := t.Q;
END_PROGRAM
