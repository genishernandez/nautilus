PROGRAM TonrSt
(* The logix dialect's TONR in Structured Text. t times IN and is cleared by
   Reset; p is the free-running pulse from the page, its own Q its Reset. *)
VAR_EXTERNAL
    IN     : BOOL;
    PT     : TIME;
    Reset  : BOOL;
    st_Q   : BOOL;
    st_ET  : TIME;
    st_P   : BOOL;
    st_PET : TIME;
END_VAR
VAR
    t : TONR;
    p : TONR;
END_VAR

t(IN := IN, PT := PT, Reset := Reset);
st_Q  := t.Q;
st_ET := t.ET;
p(IN := TRUE, PT := T#1S, Reset := p.Q);
st_P   := p.Q;
st_PET := p.ET;
END_PROGRAM
