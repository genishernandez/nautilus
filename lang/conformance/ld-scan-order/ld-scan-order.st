PROGRAM OrderSt
(* X is written above the statement that reads it (Y); W is written below
   the statement that reads it (V). *)
VAR_EXTERNAL
    A    : BOOL;
    st_X : BOOL;
    st_Y : BOOL;
    st_V : BOOL;
    st_W : BOOL;
END_VAR

st_X := A;
st_Y := st_X;
st_V := st_W;
st_W := A;
END_PROGRAM
