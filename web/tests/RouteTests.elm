module RouteTests exposing (allPassed, suite)

import App.Route as Route exposing (CollectionRef(..), ProfileRef(..), View(..), Workspace(..))
import Url


parse : String -> Route.Route
parse href =
    case Url.fromString ("http://kura.test" ++ href) of
        Just url ->
            Route.fromUrl url

        Nothing ->
            { query = "UNPARSED"
            , collection = RejectedCollection
            , post = Nothing
            , view = GridView, profile = NoProfile
            }


roundTrip : String -> String
roundTrip href =
    Route.toHref (parse href)


caseSearchRoundTrip : Bool
caseSearchRoundTrip =
    let
        route =
            parse "/?q=cats"
    in
    route.query == "cats"
        && route.collection == NoCollection
        && route.post == Nothing
        && route.view == GridView
        && Route.toHref route == "/?q=cats"
        && Route.workspace route == SearchWorkspace "cats"


caseLoupeRoundTrip : Bool
caseLoupeRoundTrip =
    let
        route =
            parse "/?q=cats&post=7&view=loupe"
    in
    route == { query = "cats", collection = NoCollection, post = Just "7", view = LoupeView, profile = NoProfile }
        && Route.toHref route == "/?q=cats&post=7&view=loupe"


caseEncodedSearchRoundTrip : Bool
caseEncodedSearchRoundTrip =
    (parse "/?q=black+cat").query == "black cat"
        && (parse "/?q=cat%26dog").query == "cat&dog"
        && roundTrip "/?q=black+cat" == "/?q=black%20cat"


caseLoupeNeedsPost : Bool
caseLoupeNeedsPost =
    (parse "/?q=cats&view=loupe").view == GridView
        && roundTrip "/?q=cats&view=loupe" == "/?q=cats"


caseCollectionRoundTrip : Bool
caseCollectionRoundTrip =
    let
        route =
            parse "/?collection=42"
    in
    route.collection == Collection "42"
        && route.query == ""
        && Route.workspace route == CollectionWorkspace "42"
        && Route.toHref route == "/?collection=42"
        && roundTrip "/?collection=42" == "/?collection=42"


caseCollectionLoupeRoundTrip : Bool
caseCollectionLoupeRoundTrip =
    let
        route =
            parse "/?collection=42&post=7&view=loupe"
    in
    route == { query = "", collection = Collection "42", post = Just "7", view = LoupeView, profile = NoProfile }
        && Route.toHref route == "/?collection=42&post=7&view=loupe"


caseCollectionWinsOverSearch : Bool
caseCollectionWinsOverSearch =
    let
        route =
            parse "/?q=cats&collection=42&post=7&view=loupe"
    in
    Route.workspace route == CollectionWorkspace "42"
        && route.query == "cats"
        && Route.toHref route == "/?collection=42&post=7&view=loupe"
        && (parse (Route.toHref route)).query == ""


caseRejectedCollectionDoesNotSearch : Bool
caseRejectedCollectionDoesNotSearch =
    let
        route =
            parse "/?collection=0&q=cats"
    in
    route.collection == RejectedCollection
        && Route.workspace route == InvalidCollection
        && Route.toHref route == "/"


caseCollectionBounds : Bool
caseCollectionBounds =
    let
        accepts id =
            Route.workspace (parse ("/?collection=" ++ id)) == CollectionWorkspace id

        rejects id =
            Route.workspace (parse ("/?collection=" ++ id)) == InvalidCollection
    in
    List.all accepts [ "1", "42", "9223372036854775807" ]
        && List.all rejects [ "0", "01", "-1", "%2B1", "1.5", "abc", "9223372036854775808", "10000000000000000000", "1e2" ]
        && Route.workspace (parse "/?collection=%2042%20") == CollectionWorkspace "42"
        && Route.forCollection "42" == Just { query = "", collection = Collection "42", post = Nothing, view = GridView, profile = NoProfile }
        && Route.forCollection "01" == Nothing


caseSearchLeavesCollection : Bool
caseSearchLeavesCollection =
    let
        collection =
            { query = "", collection = Collection "42", post = Just "7", view = LoupeView, profile = NoProfile }

        search =
            Route.forSearch collection "cats"
    in
    search == { query = "cats", collection = NoCollection, post = Just "7", view = LoupeView, profile = NoProfile }
        && Route.toHref search == "/?q=cats&post=7&view=loupe"
        && Route.workspace search == SearchWorkspace "cats"


caseDropsCursorTokenAndSelection : Bool
caseDropsCursorTokenAndSelection =
    let
        route =
            parse "/?collection=42&cursor=abc&token=secret&selected=1&selected=2"
    in
    Route.toHref route == "/?collection=42"
        && String.contains "cursor" (Route.toHref route) == False
        && String.contains "token" (Route.toHref route) == False
        && String.contains "selected" (Route.toHref route) == False


caseEmptyLibrary : Bool
caseEmptyLibrary =
    parse "/" == { query = "", collection = NoCollection, post = Nothing, view = GridView, profile = NoProfile }
        && Route.toHref (parse "/") == "/"


caseProfileRoundTrip : Bool
caseProfileRoundTrip =
    let
        route =
            parse "/u/Ada_Lane"
    in
    route.profile == Profile "ada_lane"
        && route.query == ""
        && route.collection == NoCollection
        && Route.toHref route == "/u/ada_lane"
        && roundTrip "/u/Ada_Lane" == "/u/ada_lane"
        && (parse "/u/a.b").profile == RejectedProfile
        && (parse "/u/").profile == RejectedProfile
        && (parse "/?q=cats").profile == NoProfile


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "search round trip", caseSearchRoundTrip )
    , ( "loupe round trip", caseLoupeRoundTrip )
    , ( "encoded search round trip", caseEncodedSearchRoundTrip )
    , ( "loupe requires a post", caseLoupeNeedsPost )
    , ( "collection round trip", caseCollectionRoundTrip )
    , ( "collection loupe round trip", caseCollectionLoupeRoundTrip )
    , ( "collection wins over search", caseCollectionWinsOverSearch )
    , ( "rejected collection does not become a search", caseRejectedCollectionDoesNotSearch )
    , ( "collection id bounds", caseCollectionBounds )
    , ( "search leaves the collection", caseSearchLeavesCollection )
    , ( "url drops cursor token and selection", caseDropsCursorTokenAndSelection )
    , ( "empty library url", caseEmptyLibrary )
    , ( "profile url round trip", caseProfileRoundTrip )
    ]
