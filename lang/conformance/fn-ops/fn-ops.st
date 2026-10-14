PROGRAM OpsSt
(* The operators the FBD blocks lower to, written as ST operators. *)
VAR_EXTERNAL
    A, B, C : BOOL;
    X, Y, Z : INT;
    R, RX, RY : REAL;
    T1, T2 : TIME;
    S : STRING;
    D1, D2 : DINT;
    st_AND, st_OR, st_XOR, st_NOT, st_AND3, st_OR3, st_XOR3, st_MOVEB, st_GT, st_GE, st_LT, st_LE, st_EQ, st_NE, st_RGT, st_RGE, st_RLT, st_RLE, st_REQ, st_RNE, st_TGT, st_TGE, st_TLT, st_TLE, st_TEQ, st_TNE : BOOL;
    st_ADD3, st_MUL3, st_SUB, st_DIV, st_MOD, st_MOVEI : INT;
    st_ADDR, st_MULR, st_SUBR, st_MOVER : REAL;
    st_MOVES : STRING;
    st_ADDD : DINT;
    st_ADDDEQ : BOOL;
    st_ADDDINT : INT;
    st_ADDDLINT : LINT;
END_VAR

st_AND  := A AND B;
st_OR   := A OR B;
st_XOR  := A XOR B;
st_NOT  := NOT A;
st_AND3 := A AND B AND C;
st_OR3  := A OR B OR C;
st_XOR3 := A XOR B XOR C;
st_ADD3 := X + Y + Z;
st_MUL3 := X * Y * Z;
st_SUB  := X - Y;
st_DIV  := X / Y;
st_MOD  := X MOD Y;
st_ADDR := X + R;
st_MULR := X * R;
st_SUBR := X - R;
st_MOVEB := A;
st_MOVEI := X;
st_MOVER := R;
st_MOVES := S;
st_GT := X > Y;
st_GE := X >= Y;
st_LT := X < Y;
st_LE := X <= Y;
st_EQ := X = Y;
st_NE := X <> Y;
st_RGT := RX > RY;
st_RGE := RX >= RY;
st_RLT := RX < RY;
st_RLE := RX <= RY;
st_REQ := RX = RY;
st_RNE := RX <> RY;
st_TGT := T1 > T2;
st_TGE := T1 >= T2;
st_TLT := T1 < T2;
st_TLE := T1 <= T2;
st_TEQ := T1 = T2;
st_TNE := T1 <> T2;
st_ADDD     := D1 + D2;
st_ADDDEQ   := D1 + D2 = 80000;
st_ADDDINT  := TO_INT(D1 + D2);
st_ADDDLINT := TO_LINT(D1 + D2);
END_PROGRAM
