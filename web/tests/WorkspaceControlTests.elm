module WorkspaceControlTests exposing (suite)

import App.Keyboard as Keyboard
import Domain.Query as Query
import Json.Decode as Decode


suite : List ( String, Bool )
suite =
    [ ( "sort preserves quoted tags and exclusions", Query.setOrder "score" "\"blue sky\" -tag:cat width:>=1200" == "\"blue sky\" -tag:cat width:>=1200 order:score" )
    , ( "sort replaces the previous order", Query.setOrder "score" "landscape order:newest" == "landscape order:score" )
    , ( "newest removes the score override", Query.setOrder "newest" "landscape order:score" == "landscape" )
    , ( "sort does not remove order text inside a quoted phrase", Query.setOrder "score" "\"order:score illustration\"" == "\"order:score illustration\" order:score" )
    , ( "score order reads the actual query", Query.order "tag:landscape order:score" == "score" )
    , ( "quoted order text does not change sort", Query.order "\"order:score\"" == "newest" )
    , ( "disclosure keys are handled as native controls", target "SUMMARY" == Just Keyboard.Control )
    , ( "select keys stay editable", target "SELECT" == Just Keyboard.Editable )
    ]


target : String -> Maybe Keyboard.Target
target tag =
    Decode.decodeString Keyboard.decoder
        ("{\"key\":\"Enter\",\"target\":{\"tagName\":\"" ++ tag ++ "\"}}")
        |> Result.toMaybe
        |> Maybe.map .target
