(* Two pass-through blocks: an input of a user block is stored into its
   parameter on the call, as an assignment stores into a tag. *)
FUNCTION_BLOCK PassR
VAR_INPUT
    x : REAL;
END_VAR
VAR_OUTPUT
    y : REAL;
END_VAR
y := x;
END_FUNCTION_BLOCK

FUNCTION_BLOCK PassD
VAR_INPUT
    x : DINT;
END_VAR
VAR_OUTPUT
    y : DINT;
END_VAR
y := x;
END_FUNCTION_BLOCK
